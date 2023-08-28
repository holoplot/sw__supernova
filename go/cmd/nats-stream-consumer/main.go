package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/utils/inotify"
)

type subjectConsumerConfig struct {
	Stream         string                          `json:"stream"`
	ConsumerConfig jetstream.OrderedConsumerConfig `json:"consumer_config"`
}

var (
	messagesReceived = promauto.NewCounter(prometheus.CounterOpts{
		Name: "nats_stream_consumer_messages_received_total",
		Help: "The total number of messages received from a subscription",
	})
	ncStatus = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "nats_stream_consumer_nc_status",
		Help: "The status of the NATS connection",
	})
)

func main() {
	http.Handle("/metrics", promhttp.Handler())
	go http.ListenAndServe(":9090", nil)

	NATS_URL := os.Getenv("NATS_URL")

	configFile := os.Getenv("CONFIG_FILE")

	nc, err := nats.Connect(NATS_URL, nats.MaxReconnects(-1))

	if err != nil {
		log.Fatal(err)
	}

	defer nc.Close()

	go func() {
		for {
			time.Sleep(time.Second)
			ncStatus.Set(float64(nc.Status()))
		}
	}()

	js, err := jetstream.New(nc)

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
			fmt.Println("config file changed")
			cancel()
			ctx, cancel = context.WithCancel(context.Background())
			updateConfig(ctx, js, configFile)
		case err := <-watcher.Error:
			log.Fatal(err)
		}
	}
}

func updateConfig(ctx context.Context, js jetstream.JetStream, configFile string) {
	file, err := os.Open(configFile)
	if err != nil {
		log.Fatal(err)
	}

	defer file.Close()

	decoder := json.NewDecoder(file)

	for {
		var config subjectConsumerConfig
		if err := decoder.Decode(&config); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			log.Fatal(err)
		}
		go consume(ctx, js, config)
	}

}

func consume(ctx context.Context, js jetstream.JetStream, config subjectConsumerConfig) {
	fmt.Printf("starting consumer with config %v\n", config)
	defer fmt.Printf("stopping consumer with config %v\n", config)

	stream, err := js.Stream(ctx, config.Stream)
	if err != nil {
		log.Fatal(err)
	}

	cons, err := stream.OrderedConsumer(ctx, config.ConsumerConfig)
	if err != nil {
		log.Fatal(err)
	}

	c, err := cons.Consume(
		func(m jetstream.Msg) {
			m.Ack()
			messagesReceived.Inc()
		},
		jetstream.ConsumeErrHandler(func(consumeCtx jetstream.ConsumeContext, err error) {
			fmt.Printf("error consuming message: %v\t%s\n", err, cons.CachedInfo().Name)
		}),
		jetstream.PullHeartbeat(5*time.Second),
		jetstream.PullExpiry(30*time.Second),
	)
	if err != nil {
		log.Fatal(err)
	}

	<-ctx.Done()

	c.Stop()

}
