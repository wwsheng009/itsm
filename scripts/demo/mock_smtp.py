#!/usr/bin/env python3
"""演示/验收用最小 SMTP sink：接收邮件并追加写入 MOCK_LOG（默认 /tmp/mock_smtp.log）。

用途：P0-2/SCA1 验证「按租户邮件路由」——不同租户各自指向不同 mock，
检查各自日志只包含本租户邮件（互不影响）。

运行：python3 mock_smtp.py [port]     环境变量：MOCK_LOG
"""
import os
import socketserver
import sys
import time

LOG = os.environ.get("MOCK_LOG", "/tmp/mock_smtp.log")


class Handler(socketserver.StreamRequestHandler):
    def handle(self):
        self.wfile.write(b"220 mock ESMTP ready\r\n")
        data_mode = False
        msg = []
        while True:
            line = self.rfile.readline()
            if not line:
                break
            if data_mode:
                if line.strip() == b".":
                    data_mode = False
                    with open(LOG, "a", encoding="utf-8") as fh:
                        fh.write("=== MAIL at %s ===\n" % time.strftime("%Y-%m-%d %H:%M:%S"))
                        fh.write(b"".join(msg).decode("utf-8", "replace"))
                        fh.write("\n=== END ===\n")
                    msg = []
                    self.wfile.write(b"250 OK queued\r\n")
                else:
                    msg.append(line)
                continue

            cmd = line.decode("utf-8", "replace").strip()
            upper = cmd.upper()
            if upper.startswith("EHLO") or upper.startswith("HELO"):
                self.wfile.write(b"250-mock\r\n250 OK\r\n")
            elif upper.startswith("AUTH"):
                self.wfile.write(b"235 Authentication successful\r\n")
            elif upper.startswith("MAIL FROM") or upper.startswith("RCPT TO"):
                self.wfile.write(b"250 OK\r\n")
            elif upper.startswith("DATA"):
                data_mode = True
                self.wfile.write(b"354 End data with <CR><LF>.<CR><LF>\r\n")
            elif upper.startswith("QUIT"):
                self.wfile.write(b"221 Bye\r\n")
                break
            elif upper.startswith("RSET") or upper.startswith("NOOP"):
                self.wfile.write(b"250 OK\r\n")
            else:
                self.wfile.write(b"250 OK\r\n")


class Server(socketserver.ThreadingTCPServer):
    allow_reuse_address = True
    daemon_threads = True


if __name__ == "__main__":
    port = int(sys.argv[1]) if len(sys.argv) > 1 else 2525
    print("mock smtp listening on 0.0.0.0:%d log=%s" % (port, LOG), flush=True)
    Server(("0.0.0.0", port), Handler).serve_forever()
