import assert from 'node:assert/strict'
import fs from 'node:fs/promises'
import { fileURLToPath, pathToFileURL } from 'node:url'
import ts from 'typescript'

// Execute the actual store with a fake API, clock and tab visibility. This
// catches request overlap and timing regressions without waiting in real time.
const timers = new Map()
let nextId = 0
globalThis.setTimeout = (callback, delay) => {
  const id = ++nextId
  timers.set(id, { callback, delay })
  return id
}
globalThis.clearTimeout = (id) => timers.delete(id)
globalThis.setInterval = () => ++nextId
globalThis.clearInterval = () => undefined
class TestDocument extends EventTarget {
  visibilityState = 'visible'
}
globalThis.document = new TestDocument()
const sample = { generatedAt: 'sample-1', pollSeconds: 5, radios: [], ssids: [], clients: [], neighbors: [], interfaces: [], hardware: { updatedAt: 'hardware-1' } }
let reads = 0
let read = async () => structuredClone(sample)
globalThis.__testAPI = {
  session: async () => ({ authenticated: true, configured: true, username: 'test' }),
  state: () => {
    reads++
    return read()
  },
  login: async () => undefined,
  logout: async () => undefined,
}
let source = await fs.readFile(new URL('../src/stores/app.ts', import.meta.url), 'utf8')
source = source.replace("from 'zustand'", `from '${pathToFileURL(fileURLToPath(new URL('../node_modules/zustand/esm/index.mjs', import.meta.url))).href}'`)
source = source.replace("import { api, ApiError } from '@/api'", 'const api = globalThis.__testAPI; class ApiError extends Error { status: number }')
const compiled = ts.transpileModule(source, { compilerOptions: { target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.ESNext } }).outputText
const { useApp } = await import(`data:text/javascript;base64,${Buffer.from(compiled).toString('base64')}`)
const settle = async () => {
  for (let i = 0; i < 20; i++) await Promise.resolve()
}
const tick = async (expectedDelay) => {
  assert.equal(timers.size, 1, 'only one refresh timer should be active')
  const [id, timer] = [...timers][0]
  assert.equal(timer.delay, expectedDelay)
  timers.delete(id)
  timer.callback()
  await settle()
}
const stop = useApp.getState().init()
await settle()
assert.equal(reads, 1)
const first = useApp.getState().state
await tick(5000)
assert.equal(reads, 2)
assert.equal(useApp.getState().state, first, 'unchanged samples must not trigger state renders')
sample.management = { pendingReboot: true }
await useApp.getState().refresh()
assert.equal(useApp.getState().state.management.pendingReboot, true, 'management saves should appear before the next radio sample')
sample.radios = [{ id: 2, wifi7: { operatingMode: '11AHE160', operatingWidth: 160, status: 'off' } }]
await useApp.getState().refresh()
sample.radios[0].wifi7 = { operatingMode: '11AEHT320', operatingWidth: 320, status: 'active' }
await useApp.getState().refresh()
assert.equal(useApp.getState().state.radios[0].wifi7.operatingWidth, 320, 'native operating changes should appear even when the gNMI sample is unchanged')
sample.pollSeconds = 1
sample.generatedAt = 'sample-2'
await tick(5000)
await tick(1000)

document.visibilityState = 'hidden'
document.dispatchEvent(new Event('visibilitychange'))
const beforeHidden = reads
assert.equal(timers.size, 0, 'background tabs should stop polling')
document.visibilityState = 'visible'
document.dispatchEvent(new Event('visibilitychange'))
await settle()
assert.equal(reads, beforeHidden + 1, 'visible tabs should refresh immediately')

let release
read = () =>
  new Promise((resolve) => {
    release = resolve
  })
const pending = useApp.getState().refresh()
const overlapping = useApp.getState().refresh()
assert.equal(reads, beforeHidden + 2, 'concurrent refreshes must share one request')
release({ ...sample })
await Promise.all([pending, overlapping])

read = async () => {
  throw new Error('AP restarting')
}
await tick(1000)
assert.equal(useApp.getState().connection, 'reconnecting')
await tick(2000)
await tick(4000)
await tick(8000)
await tick(16000)
await tick(30000)

read = () =>
  new Promise((resolve) => {
    release = resolve
  })
const oldSession = useApp.getState().refresh()
await useApp.getState().signOut()
release({ ...sample })
await oldSession
assert.equal(useApp.getState().state, null, 'late responses must not restore signed-out data')
assert.equal(useApp.getState().auth, 'signed-out')
stop()
assert.equal(timers.size, 0)
console.log('Live updates: sampling cadence, visibility, deduplication, unchanged samples, retry backoff and session isolation passed.')
