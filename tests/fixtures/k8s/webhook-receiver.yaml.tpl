apiVersion: v1
kind: Pod
metadata:
  name: $name
  namespace: $namespace
  labels:
    app.kubernetes.io/name: $name
spec:
  restartPolicy: Always
  containers:
    - name: receiver
      image: $image
      imagePullPolicy: IfNotPresent
      command: ["python", "-c"]
      args:
        - |
          import json
          from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

          path = "/tmp/webhook-events.jsonl"

          class Handler(BaseHTTPRequestHandler):
              def do_POST(self):
                  length = int(self.headers.get("content-length", "0"))
                  raw = self.rfile.read(length)
                  event = {
                      "headers": {key.lower(): value for key, value in self.headers.items()},
                      "path": self.path,
                      "body": json.loads(raw.decode("utf-8")),
                  }
                  with open(path, "a", encoding="utf-8") as handle:
                      handle.write(json.dumps(event, sort_keys=True) + "\n")
                  self.send_response(204)
                  self.end_headers()

              def log_message(self, *args):
                  pass

          ThreadingHTTPServer(("0.0.0.0", 8080), Handler).serve_forever()
      ports:
        - name: http
          containerPort: 8080
---
apiVersion: v1
kind: Service
metadata:
  name: $name
  namespace: $namespace
spec:
  selector:
    app.kubernetes.io/name: $name
  ports:
    - name: http
      port: 8080
      targetPort: http
