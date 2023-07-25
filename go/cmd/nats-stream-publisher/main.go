package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/utils/inotify"
)

type subjectPublisherConfig struct {
	Subject  string `json:"subject"`
	Interval int    `json:"interval"`
	MsgSize  int    `json:"msg_size"`
}

var (
	publishErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "nats_stream_publisher_publish_errors_total",
		Help: "The total number of errors publishing messages",
	})
)

func main() {
	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(":9090", nil)

	NATS_URL := os.Getenv("NATS_URL")

	configFile := os.Getenv("CONFIG_FILE")

	nc, err := nats.Connect(NATS_URL)

	if err != nil {
		log.Fatal(err)
	}

	defer nc.Close()

	js, err := nc.JetStream()

	if err != nil {
		log.Fatal(err)
	}

	watcher, err := inotify.NewWatcher()
	if err != nil {
		log.Fatal(err)
	}

	if err := watcher.AddWatch(configFile, inotify.InModify); err != nil {
		log.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	updateConfig(ctx, js, configFile)
	for {
		select {
		case <-watcher.Event:
			cancel()
			ctx, cancel = context.WithCancel(context.Background())
			updateConfig(ctx, js, configFile)
		case err := <-watcher.Error:
			log.Fatal(err)
		}
	}

}

func updateConfig(ctx context.Context, js nats.JetStreamContext, configFile string) {
	file, err := os.Open(configFile)
	if err != nil {
		log.Fatal(err)
	}

	defer file.Close()

	decoder := json.NewDecoder(file)

	for {
		var config subjectPublisherConfig
		if err := decoder.Decode(&config); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			log.Fatal(err)
		}

		go publishMsgs(ctx, js, config)
	}

}

func publishMsgs(ctx context.Context, js nats.JetStreamContext, config subjectPublisherConfig) {
	fmt.Printf("Starting to publish msgs with config %v\n", config)
	ticker := time.NewTicker(time.Duration(config.Interval) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			msg := make([]byte, config.MsgSize)
			_, err := rand.Read(msg)
			if err != nil {
				log.Fatal(err)
			}

			if _, err := js.Publish(config.Subject, msg); err != nil {
				publishErrors.Inc()
				fmt.Printf("Error publishing msg: %v\n", err)
			}

		case <-ctx.Done():
			fmt.Printf("Stopping publishing msgs with config %v\n", config)
			return
		}
	}
}
