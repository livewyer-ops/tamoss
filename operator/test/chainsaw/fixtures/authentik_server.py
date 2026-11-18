import json
import urllib.parse
import uuid
from http.server import BaseHTTPRequestHandler, HTTPServer

blueprints = {}
apply_counts = {}


class Handler(BaseHTTPRequestHandler):
    def write_json(self, status, payload):
        body = json.dumps(payload).encode()
        self.send_response(status)
        self.send_header("content-type", "application/json")
        self.send_header("content-length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        parsed = urllib.parse.urlparse(self.path)
        if parsed.path == "/api/v3/managed/blueprints/":
            if not self.authorized():
                return
            query = urllib.parse.parse_qs(parsed.query)
            name = query.get("name", [""])[0]
            results = [
                value
                for value in blueprints.values()
                if not name or value["name"] == name
            ]
            self.write_json(200, {"results": results})
            return
        if parsed.path.startswith("/debug/blueprints/"):
            name = urllib.parse.unquote(parsed.path.rsplit("/", 1)[-1])
            blueprint = blueprints.get(name, {})
            payload = blueprint.get("content", "").encode()
            self.send_response(200)
            self.send_header("content-type", "text/plain")
            self.send_header("content-length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)
            return
        if parsed.path.startswith("/debug/applies/"):
            name = urllib.parse.unquote(parsed.path.rsplit("/", 1)[-1])
            payload = str(apply_counts.get(name, 0)).encode()
            self.send_response(200)
            self.send_header("content-type", "text/plain")
            self.send_header("content-length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)
            return
        if not parsed.path.endswith("/.well-known/openid-configuration"):
            self.send_response(404)
            self.end_headers()
            return
        base = "http://authentik-chainsaw.auth.svc:9000"
        payload = {
            "issuer": base
            + parsed.path.rsplit("/.well-known/openid-configuration", 1)[0],
            "authorization_endpoint": base + "/authorize",
            "token_endpoint": base + "/token",
            "jwks_uri": base + "/jwks",
        }

        self.write_json(200, payload)

    def do_POST(self):
        parsed = urllib.parse.urlparse(self.path)
        if parsed.path == "/api/v3/managed/blueprints/":
            if not self.authorized():
                return
            request = self.read_json()
            blueprint = {
                "pk": str(uuid.uuid4()),
                "name": request["name"],
                "path": request.get("path", ""),
                "context": request.get("context", {}),
                "enabled": request.get("enabled", True),
                "content": request.get("content", ""),
                "status": "unknown",
                "managed_models": [],
                "metadata": {},
                "last_applied": "2026-01-01T00:00:00Z",
                "last_applied_hash": "",
            }
            blueprints[blueprint["name"]] = blueprint
            self.write_json(201, blueprint)
            return
        if parsed.path.startswith(
            "/api/v3/managed/blueprints/"
        ) and parsed.path.endswith("/apply/"):
            if not self.authorized():
                return
            pk = parsed.path.strip("/").split("/")[-2]
            for blueprint in blueprints.values():
                if blueprint["pk"] == pk:
                    blueprint["status"] = "successful"
                    apply_counts[blueprint["name"]] = (
                        apply_counts.get(blueprint["name"], 0) + 1
                    )
                    self.write_json(200, blueprint)
                    return
            self.send_response(404)
            self.end_headers()
            return
        self.send_response(404)
        self.end_headers()

    def do_PUT(self):
        parsed = urllib.parse.urlparse(self.path)
        if not parsed.path.startswith("/api/v3/managed/blueprints/"):
            self.send_response(404)
            self.end_headers()
            return
        if not self.authorized():
            return
        pk = parsed.path.strip("/").split("/")[-1]
        request = self.read_json()
        for blueprint in blueprints.values():
            if blueprint["pk"] == pk:
                blueprint.update(
                    {
                        "name": request["name"],
                        "path": request.get("path", ""),
                        "context": request.get("context", {}),
                        "enabled": request.get("enabled", True),
                        "content": request.get("content", ""),
                    }
                )
                self.write_json(200, blueprint)
                return
        self.send_response(404)
        self.end_headers()

    def do_DELETE(self):
        parsed = urllib.parse.urlparse(self.path)
        if not parsed.path.startswith("/api/v3/managed/blueprints/"):
            self.send_response(404)
            self.end_headers()
            return
        if not self.authorized():
            return
        pk = parsed.path.strip("/").split("/")[-1]
        for name, blueprint in list(blueprints.items()):
            if blueprint["pk"] == pk:
                blueprints.pop(name, None)
                apply_counts.pop(name, None)
                self.send_response(204)
                self.end_headers()
                return
        self.send_response(404)
        self.end_headers()

    def read_json(self):
        length = int(self.headers.get("content-length", "0"))
        return json.loads(self.rfile.read(length) or b"{}")

    def authorized(self):
        if self.headers.get("authorization") != "Bearer test-token":
            self.send_response(403)
            self.end_headers()
            return False
        return True

    def log_message(self, _format, *_args):
        return


if __name__ == "__main__":
    # The Kubernetes Service reaches this fixture through the Pod IP.
    HTTPServer(("0.0.0.0", 9000), Handler).serve_forever()  # nosec B104
