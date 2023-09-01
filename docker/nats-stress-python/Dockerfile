FROM python:3.11-alpine


WORKDIR /usr/src/app

COPY python/requirements.txt ./
RUN pip install --no-cache-dir -r requirements.txt

COPY python .

CMD [ "python", "./nats-stream-consumer.py" ]
