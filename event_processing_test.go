package main

import (
	"bytes"
	"encoding/json"
	"github.com/streadway/amqp"
	"io/ioutil"
	"os"
	"path"
	"testing"
)

func processJson(routingKey string, indata []byte) ([]map[string]interface{}, error) {
	var msg map[string]interface{}

	decoder := json.NewDecoder(bytes.NewReader(indata))

	// Ensure that we decode numbers in the JSON as integers and *not* float64s
	decoder.UseNumber()

	if err := decoder.Decode(&msg); err != nil {
		return nil, err
	}

	msgs, err := ProcessJSONMessage(msg, routingKey)
	if err != nil {
		return nil, err
	} else {
		return msgs, nil
	}
}

func marshalJson(msgs []map[string]interface{}) (string, error) {
	var ret string

	for _, msg := range msgs {
		msg["cb_server"] = "cbserver"
		marshaled, err := json.Marshal(msg)
		if err != nil {
			return "", err
		}
		ret += string(marshaled) + "\n"
	}

	return ret, nil
}

func processProtobuf(routingKey string, indata []byte) ([]map[string]interface{}, error) {
	emptyHeaders := new(amqp.Table)

	msg, err := ProcessProtobufMessage(routingKey, indata, *emptyHeaders)
	if err != nil {
		return nil, err
	} else {
		msgs := make([]map[string]interface{}, 0, 1)
		msgs = append(msgs, msg)
		return msgs, nil
	}
}

func BenchmarkProtobufEventProcessing(b *testing.B) {
	fn := path.Join("./tests/raw_data/protobuf/ingress.event.process/0.protobuf")
	fp, _ := os.Open(fn)
	d, _ := ioutil.ReadAll(fp)

	for i := 0; i < b.N; i++ {
		processProtobuf("ingress.event.process", d)
	}
}

func BenchmarkJsonEventProcessing(b *testing.B) {
	fn := path.Join("./tests/raw_data/json/watchlist.hit.process/0.json")
	fp, _ := os.Open(fn)
	d, _ := ioutil.ReadAll(fp)

	for i := 0; i < b.N; i++ {
		processJson("watchlist.hit.process", d)
	}
}

type outputMessageFunc func([]map[string]interface{}) (string, error)

func TestEventProcessing(t *testing.T) {
	t.Log("Generating JSON output to go_output...")
	processTestEvents(t, "go_output", marshalJson)
}

func processTestEvents(t *testing.T, outputDir string, outputFunc outputMessageFunc) {
	formats := [...]struct {
		formatType string
		process    func(string, []byte) ([]map[string]interface{}, error)
	}{{"json", processJson}, {"protobuf", processProtobuf}}

	config.CbServerURL = "https://cbtests/"

	for _, format := range formats {
		pathname := path.Join("./tests/raw_data", format.formatType)
		fp, err := os.Open(pathname)
		if err != nil {
			t.Logf("Could not open %s", pathname)
			t.FailNow()
		}

		infos, err := fp.Readdir(0)
		if err != nil {
			t.Logf("Could not enumerate directory %s", pathname)
			t.FailNow()
		}

		fp.Close()

		for _, info := range infos {
			if !info.IsDir() {
				continue
			}

			routingKey := info.Name()
			os.MkdirAll(path.Join("./tests", outputDir, format.formatType, routingKey), 0755)

			// process all files inside this directory
			routingDir := path.Join(pathname, info.Name())
			fp, err := os.Open(routingDir)
			if err != nil {
				t.Logf("Could not open directory %s", routingDir)
				t.FailNow()
			}

			files, err := fp.Readdir(0)
			if err != nil {
				t.Errorf("Could not enumerate directory %s; continuing", routingDir)
				continue
			}

			fp.Close()

			for _, fn := range files {
				if fn.IsDir() {
					continue
				}

				fp, err := os.Open(path.Join(routingDir, fn.Name()))
				if err != nil {
					t.Errorf("Could not open %s for reading", path.Join(routingDir, fn.Name()))
					continue
				}
				b, err := ioutil.ReadAll(fp)
				if err != nil {
					t.Errorf("Could not read %s", path.Join(routingDir, fn.Name()))
					continue
				}

				fp.Close()

				msgs, err := format.process(routingKey, b)
				if err != nil {
					t.Errorf("Error processing %s: %s", path.Join(routingDir, fn.Name()), err)
					continue
				}

				out, err := outputFunc(msgs)
				if err != nil {
					t.Errorf("Error serializing %s: %s", path.Join(routingDir, fn.Name()), err)
					continue
				}

				outfp, err := os.Create(path.Join("./tests", outputDir, format.formatType, routingKey, fn.Name()))
				if err != nil {
					t.Errorf("Error creating file: %s", err)
					continue
				}

				outfp.Write([]byte(out))
				outfp.Close()
			}
		}
	}
}
