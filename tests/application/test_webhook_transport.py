from __future__ import annotations

import ipaddress
import socket
import ssl
from concurrent.futures import ThreadPoolExecutor
from datetime import UTC, datetime, timedelta
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from threading import Event, Thread
from unittest.mock import Mock

import pytest
from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import ec
from cryptography.x509.oid import NameOID
from tamoss.application import webhooks
from urllib3.exceptions import NewConnectionError


@pytest.mark.parametrize("status", [200, 503])
def test_delivery_closes_an_unfinished_large_response_without_reading_it(status):
    release_body = Event()

    class Receiver(BaseHTTPRequestHandler):
        def do_POST(self):
            self.rfile.read(int(self.headers.get("Content-Length", 0)))
            self.send_response(status)
            self.send_header("Content-Length", str(1024**3))
            self.end_headers()
            release_body.wait(5)

        def log_message(self, *_args):
            pass

    server = ThreadingHTTPServer(("127.0.0.1", 0), Receiver)
    thread = Thread(target=server.serve_forever, daemon=True)
    thread.start()
    try:
        with ThreadPoolExecutor(max_workers=1) as executor:
            future = executor.submit(
                webhooks.send_webhook_delivery,
                webhook={"url": f"http://127.0.0.1:{server.server_port}/hook"},
                payload={"event_type": "flows/created"},
                timeout_seconds=3,
                egress_policy=webhooks.WebhookEgressPolicy(
                    allowed_hosts=("127.0.0.1",)
                ),
            )
            try:
                response = future.result(timeout=1)
                assert response.status_code == status
                assert response.raw.closed
                assert response._content_consumed is False
            finally:
                release_body.set()
    finally:
        release_body.set()
        server.shutdown()
        server.server_close()
        thread.join(timeout=5)


@pytest.mark.parametrize("scheme", ["http", "https"])
def test_connection_dials_validated_literal_and_preserves_hostname(
    monkeypatch: pytest.MonkeyPatch, scheme: str
) -> None:
    policy = webhooks.WebhookEgressPolicy()
    adapter = webhooks._http_session(policy).get_adapter(scheme + "://")
    pool = adapter.poolmanager.connection_from_url(scheme + "://receiver.example.test")
    connection = pool.ConnectionCls("receiver.example.test", port=443)
    monkeypatch.setattr(
        webhooks,
        "_resolve_host_addresses",
        lambda hostname, port: [ipaddress.ip_address("93.184.216.34")],
    )
    dial = Mock(return_value=object())
    monkeypatch.setattr("urllib3.util.connection.create_connection", dial)
    connection._new_conn()
    assert dial.call_args.args[0] == ("93.184.216.34", 443)
    assert connection.host == "receiver.example.test"
    assert connection._dns_host == "receiver.example.test"


def test_connection_rejects_rebound_address_before_opening_socket(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    policy = webhooks.WebhookEgressPolicy()
    addresses = [ipaddress.ip_address("93.184.216.34")]
    monkeypatch.setattr(
        webhooks, "_resolve_host_addresses", lambda host, port: addresses
    )
    webhooks.validate_webhook_url("https://receiver.example.test", egress_policy=policy)
    addresses[:] = [ipaddress.ip_address("127.0.0.1")]
    adapter = webhooks._http_session(policy).get_adapter("https://")
    pool = adapter.poolmanager.connection_from_url("https://receiver.example.test")
    dial = Mock()
    monkeypatch.setattr("urllib3.util.connection.create_connection", dial)
    with pytest.raises(webhooks.WebhookEgressError):
        pool.ConnectionCls("receiver.example.test")._new_conn()
    dial.assert_not_called()


def test_connection_tries_other_validated_addresses_on_connect_failure(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    policy = webhooks.WebhookEgressPolicy()
    monkeypatch.setattr(
        webhooks,
        "_resolve_host_addresses",
        lambda host, port: [
            ipaddress.ip_address("2606:4700:4700::1111"),
            ipaddress.ip_address("1.1.1.1"),
        ],
    )
    adapter = webhooks._http_session(policy).get_adapter("https://")
    pool = adapter.poolmanager.connection_from_url("https://receiver.example.test")
    connection = pool.ConnectionCls("receiver.example.test")
    dial = Mock(side_effect=[OSError("unreachable"), object()])
    monkeypatch.setattr("urllib3.util.connection.create_connection", dial)
    connection._new_conn()
    assert [call.args[0][0] for call in dial.call_args_list] == [
        "2606:4700:4700::1111",
        "1.1.1.1",
    ]
    assert connection.host == "receiver.example.test"
    dial.side_effect = OSError("unreachable")
    with pytest.raises(NewConnectionError):
        connection._new_conn()
    assert connection.host == "receiver.example.test"


def test_sessions_isolate_policies_and_ignore_ambient_proxy_credentials() -> None:
    public = webhooks._http_session(webhooks.WebhookEgressPolicy())
    private = webhooks._http_session(
        webhooks.WebhookEgressPolicy(allowed_hosts=("receiver.internal",))
    )
    assert public is not private
    assert public.trust_env is False
    assert public.cookies.get_policy().set_ok(None, None) is False


def test_https_validates_original_hostname_and_sends_sni_without_resolving_it_again(
    monkeypatch: pytest.MonkeyPatch, tmp_path: Path
) -> None:
    hostname = "receiver.example.test"
    private_key = ec.generate_private_key(ec.SECP256R1())
    name = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, hostname)])
    certificate = (
        x509.CertificateBuilder()
        .subject_name(name)
        .issuer_name(name)
        .public_key(private_key.public_key())
        .serial_number(x509.random_serial_number())
        .not_valid_before(datetime.now(UTC) - timedelta(minutes=1))
        .not_valid_after(datetime.now(UTC) + timedelta(hours=1))
        .add_extension(x509.SubjectAlternativeName([x509.DNSName(hostname)]), False)
        .add_extension(x509.BasicConstraints(ca=True, path_length=None), True)
        .sign(private_key, hashes.SHA256())
    )
    cert_path, key_path = tmp_path / "cert.pem", tmp_path / "key.pem"
    cert_path.write_bytes(certificate.public_bytes(serialization.Encoding.PEM))
    key_path.write_bytes(
        private_key.private_bytes(
            serialization.Encoding.PEM,
            serialization.PrivateFormat.PKCS8,
            serialization.NoEncryption(),
        )
    )
    received = []

    class Receiver(BaseHTTPRequestHandler):
        def do_POST(self):
            received.append(dict(self.headers))
            self.rfile.read(int(self.headers.get("Content-Length", 0)))
            self.send_response(204)
            self.send_header("Set-Cookie", "receiver-secret=private; Path=/")
            self.end_headers()

        def log_message(self, *_args):
            pass

    server = ThreadingHTTPServer(("127.0.0.1", 0), Receiver)
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.load_cert_chain(cert_path, key_path)
    server_names = []
    context.set_servername_callback(
        lambda _socket, name, _context: server_names.append(name)
    )
    server.socket = context.wrap_socket(server.socket, server_side=True)
    thread = Thread(target=server.serve_forever, daemon=True)
    thread.start()
    resolve = socket.getaddrinfo

    def literal_only(host, *args, **kwargs):
        assert host == "127.0.0.1", "HTTP transport re-resolved the receiver hostname"
        return resolve(host, *args, **kwargs)

    monkeypatch.setattr(socket, "getaddrinfo", literal_only)
    monkeypatch.setattr(
        webhooks,
        "_resolve_host_addresses",
        lambda host, port: [ipaddress.ip_address("127.0.0.1")],
    )
    policy = webhooks.WebhookEgressPolicy(allowed_hosts=(hostname,))
    session = webhooks._http_session(policy)
    try:
        for _ in range(2):
            response = session.post(
                f"https://{hostname}:{server.server_port}/events",
                json={"event_type": "flows/created"},
                verify=str(cert_path),
                timeout=5,
            )
            assert response.status_code == 204
        assert server_names == [hostname, hostname]
        assert all(
            headers["Host"] == f"{hostname}:{server.server_port}"
            for headers in received
        )
        assert all("Cookie" not in headers for headers in received)
    finally:
        session.close()
        server.shutdown()
        server.server_close()
        thread.join(timeout=5)
