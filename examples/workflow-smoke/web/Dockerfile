FROM python:3.12-slim

WORKDIR /app

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY app.py .
COPY message.txt .

ENV PORT=8080

CMD ["python", "app.py"]
