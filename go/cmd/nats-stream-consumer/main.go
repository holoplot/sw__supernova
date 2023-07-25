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
	"strings"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/utils/inotify"
)

type subjectConsumerConfig struct {
	Stream                string              `json:"stream"`
	Subject               string              `json:"subject"`
	Pull                  bool                `json:"pull"`
	UniqueDeliverySubject bool                `json:"unique_delivery_subject"`
	ConsumerConfig        nats.ConsumerConfig `json:"consumer_config"`
}

var (
	messagesReceivedPush = promauto.NewCounter(prometheus.CounterOpts{
		Name: "nats_stream_consumer_messages_received_push_total",
		Help: "The total number of messages received from a push subscription",
	})
	messagesReceivedPull = promauto.NewCounter(prometheus.CounterOpts{
		Name: "nats_stream_consumer_messages_received_pull_total",
		Help: "The total number of messages received from a pull subscription",
	})
	fetchErrorsDeadline = promauto.NewCounter(prometheus.CounterOpts{
		Name: "nats_stream_consumer_fetch_errors_deadline_total",
		Help: "The total number of deadline errors fetching messages from a pull subscription",
	})
	fetchErrorsOther = promauto.NewCounter(prometheus.CounterOpts{
		Name: "nats_stream_consumer_fetch_errors_other_total",
		Help: "The total number of other errors fetching messages from a pull subscription",
	})
	fetchErrorsConsumerDeleted = promauto.NewCounter(prometheus.CounterOpts{
		Name: "nats_stream_consumer_fetch_errors_consumer_deleted_total",
		Help: "The total number of errors fetching messages from a pull subscription where the consumer was deleted",
	})
	ncStatus = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "nats_stream_consumer_nc_status",
		Help: "The status of the NATS connection",
	})
	reSubscibes = promauto.NewCounter(prometheus.CounterOpts{
		Name: "nats_stream_consumer_re_subscribes_total",
		Help: "The total number of re-subscribes",
	})
	createConsumerErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "nats_stream_consumer_create_consumer_errors_total",
		Help: "The total number of errors creating a consumer",
	})
	createSubscriptionErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "nats_stream_consumer_create_subscription_errors_total",
		Help: "The total number of errors creating a subscription",
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
			fmt.Printf("nc status %v\n", nc.Status())
		}
	}()

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
			fmt.Println("config file changed")
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

func consume(ctx context.Context, js nats.JetStreamContext, config subjectConsumerConfig) {
	fmt.Printf("starting consumer with config %v\n", config)
	defer fmt.Printf("stopping consumer with config %v\n", config)

	if config.UniqueDeliverySubject {
		config.ConsumerConfig.DeliverSubject = nats.NewInbox()
	}

	if config.Pull {
		for {
			err := pullSubscribe(ctx, js, config)
			if errors.Is(err, context.Canceled) {
				return
			}
			fmt.Printf("pull subscribe error %v\n", err)
			reSubscibes.Inc()
		}
	}

	consumer, err := js.AddConsumer(config.Stream, &config.ConsumerConfig)
	if err != nil {
		log.Fatal(err)
	}
	defer js.DeleteConsumer(config.Stream, consumer.Name)

	opts := []nats.SubOpt{}
	opts = append(opts, nats.Bind(consumer.Stream, consumer.Name))

	subscription, err := js.Subscribe(config.Subject, func(msg *nats.Msg) {
		messagesReceivedPush.Inc()
	}, opts...)
	if err != nil {
		fmt.Println("subscribe failed")
		log.Fatal(err)
	}
	defer subscription.Unsubscribe()

	ticker := time.NewTicker(time.Second)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !subscription.IsValid() {
				log.Fatalf("subscription invalid %v\n", subscription)
			}
		}
	}

}

func pullSubscribe(ctx context.Context, js nats.JetStreamContext, config subjectConsumerConfig) error {

	consumer, err := js.AddConsumer(config.Stream, &config.ConsumerConfig)
	if err != nil {
		createConsumerErrors.Inc()
		return err
	}
	defer js.DeleteConsumer(config.Stream, consumer.Name)

	opts := []nats.SubOpt{}
	opts = append(opts, nats.Bind(consumer.Stream, consumer.Name))
	subscription, err := js.PullSubscribe(config.Subject, "", opts...)
	if err != nil {
		fmt.Println("pull subscribe failed")
		createSubscriptionErrors.Inc()

		if errors.Is(err, context.DeadlineExceeded) {
			// Not wrapping here as we don't want this to match a context exceeded error that would silently kill this consumer
			return fmt.Errorf("pull subscribe deadline exceeded")
		}

		return err
	}
	defer subscription.Unsubscribe()
	for {
		_, err = subscription.Fetch(1, nats.Context(ctx))
		if errors.Is(err, context.DeadlineExceeded) {
			fetchErrorsDeadline.Inc()
			continue
		}

		if errors.Is(err, nats.ErrConsumerDeleted) {
			fmt.Println("pull subscription fetch consumer deleted")
			fetchErrorsConsumerDeleted.Inc()
			return err
		}

		if errors.Is(err, nats.ErrDisconnected) {
			fmt.Println("pull subscription fetch server disconnected")
			return err
		}

		if errors.Is(err, context.Canceled) {
			if _, ok := <-ctx.Done(); ok {
				return err
			}
			fmt.Println("nats context canceled but not broader context ")
		}

		if err != nil {
			// Can't find the error in the nats library
			if strings.Contains(err.Error(), "nats: Server Shutdown") {
				fmt.Println("pull subscription fetch server shutdown")
				return err
			}
		}

		if err != nil {
			fmt.Printf("fetch error %s\t%T\n", err, err)
			fetchErrorsOther.Inc()

			consumerInfo, err := subscription.ConsumerInfo()
			fmt.Printf("consumer info %v %v\tsubscription valid %v\n", consumerInfo, err, subscription.IsValid())
			continue
		}
		messagesReceivedPull.Inc()
	}
}
