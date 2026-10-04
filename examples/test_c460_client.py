import json
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from c460_client import C460, APIError


class ClientTest(unittest.TestCase):
    def setUp(self):
        self.requests = []
        requests = self.requests

        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                requests.append((self.path, self.headers.get('Authorization')))
                if self.path.endswith('/redirect'):
                    self.send_response(302)
                    self.send_header('Location', '/api/v1/should-not-be-called')
                    self.end_headers()
                    return
                if self.path.endswith('/denied'):
                    self.send_response(403)
                    self.end_headers()
                    self.wfile.write(b'{"error":"permission denied"}')
                    return
                self.send_response(200)
                self.end_headers()
                self.wfile.write(json.dumps({'path': self.path}).encode())

            def do_PUT(self):
                body = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
                self.send_response(200)
                self.end_headers()
                self.wfile.write(json.dumps({'body': body, 'type': self.headers.get('Content-Type')}).encode())

            def log_message(self, *_):
                pass

        self.server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        self.thread = threading.Thread(target=self.server.serve_forever)
        self.thread.start()
        self.client = C460(f'http://127.0.0.1:{self.server.server_port}', 'test-secret')

    def tearDown(self):
        self.server.shutdown()
        self.thread.join()
        self.server.server_close()

    def test_filters_encoded_names_and_json(self):
        self.client.clients('FOH & MGMT', '6')
        self.assertEqual(self.requests[0], ('/api/v1/clients?ssid=FOH+%26+MGMT&band=6', 'Bearer test-secret'))
        result = self.client.update_ssid('FOH MGMT', {'enabled': True})
        self.assertEqual(result, {'body': {'enabled': True}, 'type': 'application/json'})

    def test_errors_and_no_redirects(self):
        with self.assertRaises(APIError) as error:
            self.client.request('GET', 'denied')
        self.assertEqual(error.exception.status, 403)
        with self.assertRaises(APIError):
            self.client.request('GET', 'redirect')
        self.assertEqual(len(self.requests), 2)

    def test_destination_validation(self):
        for endpoint in ['https://example.com', '//example.com', '../state', '%2e%2e/state', 'device\r\nInjected:yes']:
            with self.assertRaises(ValueError):
                self.client.request('GET', endpoint)
        for url in ['file:///tmp/data', 'https://user:password@example.com', 'http://example.com/other']:
            with self.assertRaises(ValueError):
                C460(url, 'token')


if __name__ == '__main__':
    unittest.main()
