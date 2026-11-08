#!/usr/bin/env python3
"""Authenticated demo smoke test for comment-attachment upload.

The test exercises the same ticket-domain endpoint used by the comment composer:
  POST /api/v1/tickets/{ticket}/attachments  (multipart; usage=comment_attachment)
then downloads the preview, verifies SHA256, and deletes the probe attachment.

The password is read from an environment variable by default; it is never
passed on the command line or written to the report.
"""

from __future__ import annotations

import argparse
import hashlib
import http.cookiejar
import json
import os
import struct
import sys
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
import zlib
from typing import Any


def make_png() -> bytes:
    """Create a valid 1x1 RGB PNG without third-party dependencies."""

    def chunk(kind: bytes, payload: bytes) -> bytes:
        data = kind + payload
        return (
            struct.pack(">I", len(payload))
            + data
            + struct.pack(">I", zlib.crc32(data) & 0xFFFFFFFF)
        )

    signature = b"\x89PNG\r\n\x1a\n"
    ihdr = struct.pack(">IIBBBBB", 1, 1, 8, 2, 0, 0, 0)
    raw = b"\x00\xff\x00\x00"  # filter=0, RGB red
    return (
        signature
        + chunk(b"IHDR", ihdr)
        + chunk(b"IDAT", zlib.compress(raw, 9))
        + chunk(b"IEND", b"")
    )


class ApiError(RuntimeError):
    def __init__(self, message: str, *, http: int | None = None, payload: Any = None):
        super().__init__(message)
        self.http = http
        self.payload = payload


class Client:
    def __init__(self, base_url: str, timeout: float):
        self.base_url = base_url.rstrip("/")
        self.timeout = timeout
        self.jar = http.cookiejar.CookieJar()
        self.opener = urllib.request.build_opener(
            urllib.request.HTTPCookieProcessor(self.jar)
        )
        self.token = ""

    def login(self, username: str, password: str) -> dict[str, Any]:
        status, payload, _headers = self._request(
            "POST",
            "/api/v1/auth/login",
            body=json.dumps({"username": username, "password": password}).encode(),
            headers={"Content-Type": "application/json"},
        )
        if status >= 400 or payload.get("code") not in (0, None):
            raise ApiError("login failed", http=status, payload=payload)
        self.token = next(
            (cookie.value for cookie in self.jar if cookie.name == "access_token"),
            "",
        )
        if not self.token:
            raise ApiError("login succeeded but access_token cookie is missing")
        return payload.get("data") or {}

    def _request(
        self,
        method: str,
        path: str,
        *,
        body: bytes | None = None,
        headers: dict[str, str] | None = None,
        auth: bool = True,
    ) -> tuple[int, Any, dict[str, str]]:
        request_headers = dict(headers or {})
        if auth and self.token:
            request_headers["Authorization"] = f"Bearer {self.token}"
        request_headers.setdefault("Accept", "application/json")
        request_headers.setdefault("User-Agent", "itsm-demo-smoke/1.0")
        req = urllib.request.Request(
            self.base_url + path,
            data=body,
            headers=request_headers,
            method=method,
        )
        try:
            with self.opener.open(req, timeout=self.timeout) as response:
                status = response.status
                raw = response.read()
                response_headers = {k.lower(): v for k, v in response.headers.items()}
        except urllib.error.HTTPError as exc:
            status = exc.code
            raw = exc.read()
            response_headers = {k.lower(): v for k, v in exc.headers.items()}
        try:
            payload = json.loads(raw.decode("utf-8")) if raw else {}
        except UnicodeDecodeError:
            payload = {"raw_bytes": len(raw)}
        if status >= 400:
            raise ApiError(
                f"HTTP {status} for {method} {path}",
                http=status,
                payload=payload,
            )
        return status, payload, response_headers

    def json(self, method: str, path: str, body: Any = None) -> Any:
        data = None
        headers: dict[str, str] = {}
        if body is not None:
            data = json.dumps(body).encode("utf-8")
            headers["Content-Type"] = "application/json"
        _status, payload, _headers = self._request(
            method, path, body=data, headers=headers
        )
        if isinstance(payload, dict) and payload.get("code") not in (0, None):
            raise ApiError(
                f"API error for {method} {path}",
                payload=payload,
            )
        return payload.get("data") if isinstance(payload, dict) else payload

    def multipart(
        self,
        path: str,
        fields: dict[str, str],
        filename: str,
        content: bytes,
        content_type: str,
    ) -> Any:
        boundary = f"----itsm-smoke-{uuid.uuid4().hex}"
        parts: list[bytes] = []
        for key, value in fields.items():
            parts.append(
                f"--{boundary}\r\n".encode()
                + f'Content-Disposition: form-data; name="{key}"\r\n\r\n'.encode()
                + value.encode()
                + b"\r\n"
            )
        parts.append(
            f"--{boundary}\r\n".encode()
            + (
                f'Content-Disposition: form-data; name="file"; filename="{filename}"\r\n'
            ).encode()
            + f"Content-Type: {content_type}\r\n\r\n".encode()
            + content
            + b"\r\n"
        )
        parts.append(f"--{boundary}--\r\n".encode())
        _status, payload, _headers = self._request(
            "POST",
            path,
            body=b"".join(parts),
            headers={"Content-Type": f"multipart/form-data; boundary={boundary}"},
        )
        if isinstance(payload, dict) and payload.get("code") not in (0, None):
            raise ApiError("attachment upload API error", payload=payload)
        return payload.get("data") if isinstance(payload, dict) else payload

    def download(self, path: str) -> tuple[bytes, int]:
        request_headers = {
            "Authorization": f"Bearer {self.token}",
            "User-Agent": "itsm-demo-smoke/1.0",
        }
        req = urllib.request.Request(
            self.base_url + path, headers=request_headers, method="GET"
        )
        try:
            with self.opener.open(req, timeout=self.timeout) as response:
                return response.read(), response.status
        except urllib.error.HTTPError as exc:
            raise ApiError(
                f"attachment download failed: HTTP {exc.code}",
                http=exc.code,
                payload={"raw_bytes": len(exc.read())},
            ) from exc


def first_ticket_id(client: Client) -> int | None:
    data = client.json("GET", "/api/v1/tickets?page=1&pageSize=1")
    if not isinstance(data, dict):
        return None
    items = data.get("items") or data.get("tickets") or []
    if not items:
        return None
    ticket_id = items[0].get("id")
    return int(ticket_id) if ticket_id else None


def write_report(path: str, report: dict[str, Any]) -> None:
    if not path:
        return
    os.makedirs(os.path.dirname(os.path.abspath(path)), exist_ok=True)
    tmp = path + ".tmp"
    with open(tmp, "w", encoding="utf-8") as handle:
        json.dump(report, handle, indent=2, ensure_ascii=False)
        handle.write("\n")
    os.replace(tmp, path)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--base-url", default="http://127.0.0.1:8089")
    parser.add_argument("--admin-user", default="admin")
    parser.add_argument("--password-env", default="DEMO_ADMIN_PASSWORD")
    parser.add_argument("--ticket-id", type=int, default=0)
    parser.add_argument("--report", default="")
    parser.add_argument("--timeout", type=float, default=30.0)
    args = parser.parse_args(argv)

    password = os.environ.get(args.password_env, "")
    if not password:
        print(
            json.dumps(
                {
                    "ok": False,
                    "status": "failed",
                    "error": f"missing password environment variable: {args.password_env}",
                },
                ensure_ascii=False,
            ),
            file=sys.stderr,
        )
        return 2

    started = time.time()
    client = Client(args.base_url, args.timeout)
    report: dict[str, Any] = {
        "ok": False,
        "status": "failed",
        "baseUrl": args.base_url,
        "ticketId": None,
        "attachmentId": None,
        "deleted": False,
    }

    try:
        client.login(args.admin_user, password)
        ticket_id = args.ticket_id or first_ticket_id(client)
        if not ticket_id:
            report.update(status="skipped", ok=True, reason="no visible ticket for attachment smoke")
            print(json.dumps(report, ensure_ascii=False, indent=2))
            write_report(args.report, report)
            return 0
        report["ticketId"] = ticket_id

        filename = f"deploy-smoke-{time.strftime('%Y%m%d-%H%M%S')}.png"
        content = make_png()
        uploaded = client.multipart(
            f"/api/v1/tickets/{ticket_id}/attachments",
            {"usage": "comment_attachment"},
            filename,
            content,
            "image/png",
        )
        attachment_id = uploaded.get("id") if isinstance(uploaded, dict) else None
        if not attachment_id:
            raise ApiError("upload response has no attachment id", payload=uploaded)
        report["attachmentId"] = int(attachment_id)

        preview_path = uploaded.get("previewUrl") or uploaded.get("fileUrl")
        if not preview_path:
            raise ApiError("upload response has neither previewUrl nor fileUrl")
        preview_path = urllib.parse.urlparse(preview_path).path
        downloaded, http_status = client.download(preview_path)
        report["previewHttp"] = http_status
        report["previewSha256"] = hashlib.sha256(downloaded).hexdigest()
        report["sha256Match"] = downloaded == content
        if not report["sha256Match"]:
            raise ApiError("downloaded preview does not match uploaded bytes")

        deleted = client.json(
            "DELETE", f"/api/v1/tickets/{ticket_id}/attachments/{attachment_id}"
        )
        report["deleted"] = True
        report["deleteResponse"] = deleted
        report.update(
            status="passed",
            ok=True,
            fileName=filename,
            fileSize=len(content),
            durationMs=int((time.time() - started) * 1000),
        )
        print(json.dumps(report, ensure_ascii=False, indent=2))
        write_report(args.report, report)
        return 0
    except (ApiError, OSError, ValueError) as exc:
        report.update(
            error=str(exc),
            durationMs=int((time.time() - started) * 1000),
        )
        if isinstance(exc, ApiError) and exc.payload is not None:
            report["response"] = exc.payload
        print(json.dumps(report, ensure_ascii=False, indent=2), file=sys.stderr)
        write_report(args.report, report)
        return 1
    finally:
        attachment_id = report.get("attachmentId")
        if attachment_id and not report.get("deleted"):
            try:
                client.json(
                    "DELETE",
                    f"/api/v1/tickets/{report['ticketId']}/attachments/{attachment_id}",
                )
            except Exception:  # noqa: BLE001 - best-effort cleanup only
                pass


if __name__ == "__main__":
    raise SystemExit(main())