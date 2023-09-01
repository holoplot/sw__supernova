# Python client

Notes:
- it's not possible to create a Consumer without creating a subscription
  using the Python library, so CREATE_CONSUMER_ERRORS metric is not used
- when fetching messages from a pull consumer, the Python library doesn't
  expose errors in the same way as the Go library, thus we are using only
  the FETCH_ERRORS_OTHER metric

Install:
```bash
python3 -m venv .venv
source .venv/bin/activate
pip install -r requirements.txt
```

Run:
```bash
export NATS_URL=...
export CONFIG_FILE=...
source .venv/bin/activate
python3 nats-stream-publisher.py
```
