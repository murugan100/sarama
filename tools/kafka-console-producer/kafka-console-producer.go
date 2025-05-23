package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/rcrowley/go-metrics"

	"github.com/murugan100/sarama"
	"github.com/murugan100/sarama/tools/tls"
)

var (
	brokerList    = flag.String("brokers", os.Getenv("KAFKA_PEERS"), "The comma separated list of brokers in the Kafka cluster. You can also set the KAFKA_PEERS environment variable")
	headers       = flag.String("headers", "", "The headers of the message to produce. Example: -headers=foo:bar,bar:foo")
	topic         = flag.String("topic", "", "REQUIRED: the topic to produce to")
	key           = flag.String("key", "", "The key of the message to produce. Can be empty.")
	value         = flag.String("value", "", "REQUIRED: the value of the message to produce. You can also provide the value on stdin.")
	partitioner   = flag.String("partitioner", "", "The partitioning scheme to use. Can be `hash`, `manual`, or `random`")
	partition     = flag.Int("partition", -1, "The partition to produce to.")
	verbose       = flag.Bool("verbose", false, "Turn on sarama logging to stderr")
	showMetrics   = flag.Bool("metrics", false, "Output metrics on successful publish to stderr")
	silent        = flag.Bool("silent", false, "Turn off printing the message's topic, partition, and offset to stdout")
	tlsEnabled    = flag.Bool("tls-enabled", false, "Whether to enable TLS")
	tlsSkipVerify = flag.Bool("tls-skip-verify", false, "Whether skip TLS server cert verification")
	tlsClientCert = flag.String("tls-client-cert", "", "Client cert for client authentication (use with -tls-enabled and -tls-client-key)")
	tlsClientKey  = flag.String("tls-client-key", "", "Client key for client authentication (use with tls-enabled and -tls-client-cert)")

	logger = log.New(os.Stderr, "", log.LstdFlags)
)

func main() {
	flag.Parse()

	if *brokerList == "" {
		printUsageErrorAndExit("no -brokers specified. Alternatively, set the KAFKA_PEERS environment variable")
	}

	if *topic == "" {
		printUsageErrorAndExit("no -topic specified")
	}

	if *verbose {
		sarama.Logger = logger
	}

	config := sarama.NewConfig()
	config.Producer.RequiredAcks = sarama.WaitForAll
	config.Producer.Return.Successes = true

	if *tlsEnabled {
		tlsConfig, err := tls.NewConfig(*tlsClientCert, *tlsClientKey)
		if err != nil {
			printErrorAndExit(69, "Failed to create TLS config: %s", err)
		}

		config.Net.TLS.Enable = true
		config.Net.TLS.Config = tlsConfig
		config.Net.TLS.Config.InsecureSkipVerify = *tlsSkipVerify
	}

	switch *partitioner {
	case "":
		if *partition >= 0 {
			config.Producer.Partitioner = sarama.NewManualPartitioner
		} else {
			config.Producer.Partitioner = sarama.NewHashPartitioner
		}
	case "hash":
		config.Producer.Partitioner = sarama.NewHashPartitioner
	case "random":
		config.Producer.Partitioner = sarama.NewRandomPartitioner
	case "manual":
		config.Producer.Partitioner = sarama.NewManualPartitioner
		if *partition == -1 {
			printUsageErrorAndExit("-partition is required when partitioning manually")
		}
	default:
		printUsageErrorAndExit(fmt.Sprintf("Partitioner %s not supported.", *partitioner))
	}

	message := &sarama.ProducerMessage{Topic: *topic, Partition: int32(*partition)}

	if *key != "" {
		message.Key = sarama.StringEncoder(*key)
	}

	if *value != "" {
		message.Value = sarama.StringEncoder(*value)
	} else if stdinAvailable() {
		bytes, err := io.ReadAll(os.Stdin)
		if err != nil {
			printErrorAndExit(66, "Failed to read data from the standard input: %s", err)
		}
		message.Value = sarama.ByteEncoder(bytes)
	} else {
		printUsageErrorAndExit("-value is required, or you have to provide the value on stdin")
	}

	if *headers != "" {
		var hdrs []sarama.RecordHeader
		arrHdrs := strings.Split(*headers, ",")
		for _, h := range arrHdrs {
			if header := strings.Split(h, ":"); len(header) != 2 {
				printUsageErrorAndExit("-header should be key:value. Example: -headers=foo:bar,bar:foo")
			} else {
				hdrs = append(hdrs, sarama.RecordHeader{
					Key:   []byte(header[0]),
					Value: []byte(header[1]),
				})
			}
		}

		if len(hdrs) != 0 {
			message.Headers = hdrs
		}
	}

	producer, err := sarama.NewSyncProducer(strings.Split(*brokerList, ","), config)
	if err != nil {
		printErrorAndExit(69, "Failed to open Kafka producer: %s", err)
	}
	defer func() {
		if err := producer.Close(); err != nil {
			logger.Println("Failed to close Kafka producer cleanly:", err)
		}
	}()

	partition, offset, err := producer.SendMessage(message)
	if err != nil {
		printErrorAndExit(69, "Failed to produce message: %s", err)
	} else if !*silent {
		fmt.Printf("topic=%s\tpartition=%d\toffset=%d\n", *topic, partition, offset)
	}
	if *showMetrics {
		metrics.WriteOnce(config.MetricRegistry, os.Stderr)
	}
}

func printErrorAndExit(code int, format string, values ...interface{}) {
	fmt.Fprintf(os.Stderr, "ERROR: %s\n", fmt.Sprintf(format, values...))
	fmt.Fprintln(os.Stderr)
	os.Exit(code)
}

func printUsageErrorAndExit(message string) {
	fmt.Fprintln(os.Stderr, "ERROR:", message)
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "Available command line options:")
	flag.PrintDefaults()
	os.Exit(64)
}

func stdinAvailable() bool {
	stat, _ := os.Stdin.Stat()
	return (stat.Mode() & os.ModeCharDevice) == 0
}
