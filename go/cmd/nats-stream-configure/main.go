package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"k8s.io/utils/inotify"
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

	updateConfig(js, configFile)
	for {
		select {
		case <-watcher.Event:
			updateConfig(js, configFile)
		case err := <-watcher.Error:
			log.Fatal(err)
		}
	}
}

func updateConfig(js nats.JetStreamContext, configFile string) {
	file, err := os.Open(configFile)
	if err != nil {
		log.Fatal(err)
	}

	defer file.Close()

	decoder := json.NewDecoder(file)

	for {
		config := &nats.StreamConfig{}
		if err = decoder.Decode(config); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}

			log.Fatal(err)
		}
		data, _ := json.Marshal(config)
		fmt.Printf("%s\n", data)
		createOrUpdateStream(js, config)
	}

}

// createOrUpdateStream creates or updates a stream with the given name and configuration.
func createOrUpdateStream(js nats.JetStreamContext, config *nats.StreamConfig) {
	_, err := js.AddStream(config)

	if errors.Is(err, nats.ErrStreamNameAlreadyInUse) {
		log.Println("Stream already exists updating configuration")
		_, err = js.UpdateStream(config)
	}

	if err != nil {
		log.Fatal(err)
	}
	log.Println("Stream created/updated")
}
