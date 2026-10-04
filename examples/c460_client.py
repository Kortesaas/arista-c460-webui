#!/usr/bin/env python3
"""Standard-library client for the C460 local API. See docs/api.md."""
import argparse
import json
import os
import ssl
import sys
import urllib.error
import urllib.parse
import urllib.request


class APIError(Exception):
    def __init__(self, status, message):
        super().__init__(f"HTTP {status}: {message}")
        self.status = status


class NoRedirect(urllib.request.HTTPRedirectHandler):
    # Never forward the integration token to a redirected address.
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


class C460:
    def __init__(self, url, token, ca_file=None, timeout=30):
        origin = urllib.parse.urlsplit(url)
        if origin.scheme not in ("http", "https") or not origin.hostname or origin.username or origin.password or origin.query or origin.fragment or origin.path not in ("", "/"):
            raise ValueError("URL must be an HTTP(S) origin without credentials, path, query or fragment")
        if not token or "\r" in token or "\n" in token:
            raise ValueError("Set C460_TOKEN to your integration token")
        self.base = url.rstrip("/") + "/api/v1/"
        self.token = token
        self.timeout = timeout
        context = ssl.create_default_context(cafile=ca_file)
        self.opener = urllib.request.build_opener(NoRedirect, urllib.request.HTTPSHandler(context=context))

    def request(self, method, endpoint, body=None):
        if not endpoint or endpoint.startswith("/") or "://" in endpoint or "#" in endpoint or "\r" in endpoint or "\n" in endpoint:
            raise ValueError("Endpoint must be relative to /api/v1, e.g. clients?band=6")
        if ".." in urllib.parse.unquote(endpoint).split("/"):
            raise ValueError("Parent path segments are not allowed")
        headers = {"Authorization": "Bearer " + self.token, "Accept": "application/json"}
        data = None
        if body is not None:
            data = json.dumps(body).encode()
            headers["Content-Type"] = "application/json"
        req = urllib.request.Request(self.base + endpoint, data=data, headers=headers, method=method.upper())
        try:
            with self.opener.open(req, timeout=self.timeout) as response:
                return json.load(response)
        except urllib.error.HTTPError as error:
            try:
                detail = json.load(error).get("error", error.reason)
            except (ValueError, AttributeError):
                detail = error.reason
            raise APIError(error.code, str(detail)) from None

    def state(self):
        return self.request("GET", "state")

    def clients(self, ssid=None, band=None):
        query = urllib.parse.urlencode({k: v for k, v in {"ssid": ssid, "band": band}.items() if v is not None})
        return self.request("GET", "clients" + ("?" + query if query else ""))

    def update_ssid(self, name, settings):
        return self.request("PUT", "ssids/" + urllib.parse.quote(name, safe=""), settings)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("method", choices=["GET", "POST", "PUT", "DELETE"])
    parser.add_argument("endpoint", help="Relative endpoint, e.g. device or clients?band=6")
    parser.add_argument("--body", help="JSON file, or - for stdin; POST/PUT otherwise send {}")
    args = parser.parse_args()
    try:
        body = {} if args.method in ("POST", "PUT") else None
        if args.body:
            if args.body == "-":
                body = json.load(sys.stdin)
            else:
                with open(args.body, encoding="utf-8") as source:
                    body = json.load(source)
        client = C460(os.environ.get("C460_URL", "https://192.168.99.40"), os.environ.get("C460_TOKEN", ""), os.environ.get("C460_CA"))
        print(json.dumps(client.request(args.method, args.endpoint, body), indent=2))
    except (APIError, ValueError, OSError, urllib.error.URLError) as error:
        print(str(error), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
