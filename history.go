package main

// In-memory history for the graphs: one point per minute for the last 24
// hours, plus a short signal track per client. Every buffer has a fixed
// maximum size, so memory use is bounded no matter how long the AP runs.

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	historyPoints       = 24 * 60
	clientTrackPoints   = 120 // two hours per client
	clientTracksTracked = 256
)

type HistoryPoint struct {
	T       int64              `json:"t"` // start of the minute, Unix seconds
	Clients int                `json:"clients"`
	PerSSID map[string]int     `json:"perSsid"`
	Util    map[string]float64 `json:"util"` // band -> average channel utilisation %
	RxBps   *float64           `json:"rxBps"`
	TxBps   *float64           `json:"txBps"`
	TempC   *float64           `json:"tempC"`
}

type ClientSample struct {
	T      int64    `json:"t"`
	RSSI   *float64 `json:"rssi"`
	Band   string   `json:"band"`
	SSID   string   `json:"ssid"`
	TxRate *float64 `json:"txRate"`
	RxRate *float64 `json:"rxRate"`
}

type clientTrack struct {
	samples  []ClientSample
	lastSeen time.Time
}

// minuteAccumulator collects the samples of the current minute.
type minuteAccumulator struct {
	start            int64
	point            HistoryPoint
	utilSum          map[string]float64
	utilN            map[string]int
	firstRx, firstTx float64
	lastRx, lastTx   float64
	firstT, lastT    time.Time
	counters         bool
}

type History struct {
	mu      sync.Mutex
	points  []HistoryPoint
	current *minuteAccumulator
	carry   *minuteAccumulator // counters at the end of the previous minute
	clients map[string]*clientTrack
}

func NewHistory() *History {
	return &History{clients: map[string]*clientTrack{}}
}

func uplinkOctets(st APState) (rx, tx float64) {
	for _, iface := range st.Interfaces {
		rx += iface.InOctets
		tx += iface.OutOctets
	}
	return rx, tx
}

// Observe adds one poller sample.
func (h *History) Observe(st APState) {
	if st.GeneratedAt.IsZero() || st.Error != "" {
		return
	}
	now := st.GeneratedAt
	minute := now.Truncate(time.Minute).Unix()
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.current != nil && h.current.start != minute {
		h.finish()
	}
	if h.current == nil {
		h.current = &minuteAccumulator{start: minute, point: HistoryPoint{T: minute, PerSSID: map[string]int{}, Util: map[string]float64{}},
			utilSum: map[string]float64{}, utilN: map[string]int{}}
		if c := h.carry; c != nil {
			h.current.counters, h.current.firstRx, h.current.firstTx, h.current.firstT = true, c.firstRx, c.firstTx, c.firstT
		}
	}
	acc := h.current
	// Peaks matter more than averages for clients and temperature.
	acc.point.Clients = max(acc.point.Clients, len(st.Clients))
	perSSID := map[string]int{}
	for _, c := range st.Clients {
		perSSID[c.SSID]++
	}
	for name, n := range perSSID {
		acc.point.PerSSID[name] = max(acc.point.PerSSID[name], n)
	}
	for _, r := range st.Radios {
		if r.Utilization != nil {
			acc.utilSum[r.Band] += *r.Utilization
			acc.utilN[r.Band]++
		}
	}
	if t := st.Device.TemperatureC; t != nil && (acc.point.TempC == nil || *t > *acc.point.TempC) {
		v := *t
		acc.point.TempC = &v
	}
	rx, tx := uplinkOctets(st)
	if !acc.counters {
		acc.firstRx, acc.firstTx, acc.firstT, acc.counters = rx, tx, now, true
	}
	acc.lastRx, acc.lastTx, acc.lastT = rx, tx, now

	for _, c := range st.Clients {
		track := h.clients[c.MAC]
		if track == nil {
			if len(h.clients) >= clientTracksTracked {
				h.evictOldestClient()
			}
			track = &clientTrack{}
			h.clients[c.MAC] = track
		}
		track.lastSeen = now
		sample := ClientSample{T: minute, RSSI: c.RSSI, Band: c.Band, SSID: c.SSID, TxRate: c.TxRate, RxRate: c.RxRate}
		if n := len(track.samples); n > 0 && track.samples[n-1].T == minute {
			// Keep the weakest signal of the minute; roaming shows as a band change.
			last := track.samples[n-1]
			if last.RSSI != nil && (sample.RSSI == nil || *last.RSSI < *sample.RSSI) {
				sample.RSSI = last.RSSI
			}
			track.samples[n-1] = sample
		} else {
			track.samples = append(track.samples, sample)
			if len(track.samples) > clientTrackPoints {
				track.samples = append([]ClientSample(nil), track.samples[len(track.samples)-clientTrackPoints:]...)
			}
		}
	}
}

func (h *History) evictOldestClient() {
	var oldest string
	var at time.Time
	for mac, t := range h.clients {
		if oldest == "" || t.lastSeen.Before(at) {
			oldest, at = mac, t.lastSeen
		}
	}
	delete(h.clients, oldest)
}

func (acc *minuteAccumulator) snapshot() HistoryPoint {
	p := acc.point
	p.Util = map[string]float64{}
	for band, sum := range acc.utilSum {
		p.Util[band] = sum / float64(acc.utilN[band])
	}
	p.PerSSID = make(map[string]int, len(acc.point.PerSSID))
	for k, v := range acc.point.PerSSID {
		p.PerSSID[k] = v
	}
	if secs := acc.lastT.Sub(acc.firstT).Seconds(); secs >= 1 {
		// Counters drop when an interface resets; skip that minute.
		if d := acc.lastRx - acc.firstRx; d >= 0 {
			v := d / secs
			p.RxBps = &v
		}
		if d := acc.lastTx - acc.firstTx; d >= 0 {
			v := d / secs
			p.TxBps = &v
		}
	}
	return p
}

func (h *History) finish() {
	h.points = append(h.points, h.current.snapshot())
	if len(h.points) > historyPoints {
		h.points = append([]HistoryPoint(nil), h.points[len(h.points)-historyPoints:]...)
	}
	// Rates span minute boundaries: start the next minute at this one's end.
	next := &minuteAccumulator{counters: h.current.counters, firstRx: h.current.lastRx, firstTx: h.current.lastTx, firstT: h.current.lastT}
	h.current = nil
	h.carry = next
}

// Points returns the history since the given time, including the minute in progress.
func (h *History) Points(since time.Time) []HistoryPoint {
	h.mu.Lock()
	defer h.mu.Unlock()
	from := since.Unix()
	i := sort.Search(len(h.points), func(i int) bool { return h.points[i].T >= from })
	out := append([]HistoryPoint{}, h.points[i:]...)
	if h.current != nil {
		out = append(out, h.current.snapshot())
	}
	return out
}

func (h *History) Client(mac string) []ClientSample {
	h.mu.Lock()
	defer h.mu.Unlock()
	if t := h.clients[strings.ToUpper(mac)]; t != nil {
		return append([]ClientSample{}, t.samples...)
	}
	return []ClientSample{}
}

func (a *API) historyHandler(w http.ResponseWriter, r *http.Request) {
	hours, err := strconv.Atoi(r.URL.Query().Get("hours"))
	if err != nil || hours < 1 || hours > 24 {
		hours = 24
	}
	reply(w, http.StatusOK, map[string]any{"points": a.history.Points(time.Now().Add(-time.Duration(hours) * time.Hour)), "intervalSeconds": 60})
}

func (a *API) clientHistory(w http.ResponseWriter, r *http.Request) {
	reply(w, http.StatusOK, map[string]any{"samples": a.history.Client(r.PathValue("mac"))})
}
