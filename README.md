# sw__supernova
Scale testing utilities to make core explode


## nats stream configure

Creates/updates jetstream streams according to ndjson config (one line one
stream). The JSONobject is defined by the [StreamConfig
type](https://pkg.go.dev/github.com/nats-io/nats.go#StreamConfig). It is
designed to be a long lived process monitoring for config file updates so it can
be ued for instance in configuration with a K8S config map mounted into the
container so changing the config map will result in the jetstream config being
updated.

## nats stream publisher

Generates nats messages according to an ndjson configuration (one line one
generator). The config file is monitored for changes with the intent of being in
used in combination with a K8S config map. The interval in millisecond and the
size of the messages in bytes can be specified with the subject on each line.

## nats stream consumer

Consumes nats messages with fully configurable consumers configured via ndjson
(one consumer one line). The config file can be updated at runtime at which
point consumers will be deleted and recreated according to the new
configuration. See the example for the configuration options the consumer config
is defined by [this
struct](https://pkg.go.dev/github.com/nats-io/nats.go#ConsumerConfig) and more
information can be found about consumers
[here](https://docs.nats.io/nats-concepts/jetstream/consumers); watch out for
the json marshaller implementation for enum fields.

For push subscription setting `unique_delivery_subject: true` will create a
unique inbox for each consumer allow for multiple instances.

## Use for testing

1. Deploy a K8S cluster (kind will be assumed in this documentation, however other such as minikube and k3s will also work)
2. Deploy a NATS cluster in k3s using the helm chart and a values file from `nats-configs` e.g. `helm install nats nats/nats --version 1.0.3 -f nats-configs/3-node-cluster-jetstream.yaml`
3. Deploy a prometheus server using the community helm chart and the values file `prometheus-values.yaml`
4. Build the docker image `docker build . -f docker/nats-stress/Dockerfile --tag nats-stress:my-image-tag`
5. Load the image into the cluster or push it somewhere `kind load docker-image nats-stress:my-image-tag`
6. Deploy the stresser helm chart using values from your desired scenario and pointing to the image you built. You may also override values such as the quantity of replicas. ` helm install nats-stresser ./charts/nats-stresser/ -f nats-load-scenarios/pull.yaml --set image.tag=my-image-tag` You can upgrade this helm release at any point if you want to change parameters.
7. Open the prometheus interface in your browser and get a feel for what is normal
8. Have fun creating and observing chaos doing for example `kubectl delete pod nats-0`