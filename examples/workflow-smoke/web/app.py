import os
import time

import pymysql
from flask import Flask

app = Flask(__name__)


def read_message() -> str:
    message_path = os.getenv("MESSAGE_PATH", "/app/message.txt")
    with open(message_path, "r", encoding="utf-8") as fh:
        return fh.read().strip()


def fetch_seed_data() -> tuple[int, str]:
    conn = pymysql.connect(
        host=os.getenv("DB_HOST", "db"),
        port=int(os.getenv("DB_PORT", "3306")),
        user=os.getenv("DB_USER", "app"),
        password=os.getenv("DB_PASSWORD", "app_password"),
        database=os.getenv("DB_NAME", "app"),
        connect_timeout=2,
        read_timeout=2,
        write_timeout=2,
        autocommit=True,
    )
    try:
        with conn.cursor() as cursor:
            cursor.execute("SELECT COUNT(*), COALESCE(MIN(label), '') FROM seed_data")
            result = cursor.fetchone()
            if result is None:
                return 0, ""
            count, label = result
            return int(count), str(label)
    finally:
        conn.close()


@app.get("/")
def index():
    message = read_message()
    last_error = ""
    for _ in range(10):
        try:
            seed_count, seed_label = fetch_seed_data()
            body = "\n".join(
                [
                    message,
                    f"seed_count={seed_count}",
                    f"seed_label={seed_label}",
                    "",
                ]
            )
            return body, 200, {"Content-Type": "text/plain; charset=utf-8"}
        except Exception as exc:  # noqa: BLE001
            last_error = str(exc)
            time.sleep(1)
    body = "\n".join(
        [
            message,
            f"db_error={last_error}",
            "",
        ]
    )
    return body, 500, {"Content-Type": "text/plain; charset=utf-8"}


if __name__ == "__main__":
    port = int(os.getenv("PORT", "8080"))
    app.run(host="0.0.0.0", port=port)
