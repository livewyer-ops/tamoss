import json
import time
from http.server import BaseHTTPRequestHandler, HTTPServer
from pathlib import Path

import jwt
from cryptography.hazmat.primitives.asymmetric import rsa

ISSUER = "http://oidc:8080"
key = rsa.generate_private_key(public_exponent=65537, key_size=2048)
jwk = json.loads(jwt.algorithms.RSAAlgorithm.to_jwk(key.public_key()))
jwk.update(kid="fixture", use="sig", alg="RS256")
documents = {
    "/.well-known/openid-configuration": {
        "issuer": ISSUER,
        "jwks_uri": ISSUER + "/jwks",
        "authorization_endpoint": ISSUER + "/authorize",
        "token_endpoint": ISSUER + "/token",
        "response_types_supported": ["id_token"],
        "subject_types_supported": ["public"],
        "id_token_signing_alg_values_supported": ["RS256"],
    },
    "/jwks": {"keys": [jwk]},
}


def write_tokens():
    now = int(time.time())
    claims = {
        "iss": ISSUER,
        "sub": "tamoss-fixture",
        "aud": "tamoss",
        "iat": now,
        "exp": now + 3600,
        "groups": ["tamoss-media"],
    }
    for name, overrides in {
        "token": {},
        "expired": {"exp": now - 3600},
        "wrong-audience": {"aud": "other"},
        "wrong-issuer": {"iss": "http://other"},
        "readonly": {"sub": "readonly-fixture", "groups": ["readonly"]},
    }.items():
        token = jwt.encode(
            claims | overrides, key, algorithm="RS256", headers={"kid": "fixture"}
        )
        target = Path("/tokens") / name
        temporary = target.with_suffix(".new")
        temporary.write_text(token)
        temporary.replace(target)


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        document = documents.get(self.path)
        if document is None:
            self.send_error(404)
            return
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(json.dumps(document).encode())

    def log_message(self, *_args):
        pass


if __name__ == "__main__":
    write_tokens()
    # The Kubernetes Service reaches this fixture through the Pod IP.
    HTTPServer(("0.0.0.0", 8080), Handler).serve_forever()  # nosec B104
