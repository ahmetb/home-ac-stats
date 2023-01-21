package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"

	"contrib.go.opencensus.io/exporter/stackdriver"
	"go.opencensus.io/stats"
	"go.opencensus.io/stats/view"
	"go.opencensus.io/tag"
)

func main() {
	defer func() { fmt.Println("success") }()
	apiKey := os.Getenv("SENSIBO_API_KEY")
	if apiKey == "" {
		log.Fatal("SENSIBO_API_KEY not set")
	}
	outsideTempMetric := stats.Float64("outside_temp", "Outside temperature in Celsius", "C")
	roomTemp := stats.Float64("room_temp", "The room temperature in Celsius", "C")
	acState := stats.Int64("ac_state", "AC state (on=1, off=0)", "state")
	roomKey := tag.MustNewKey("room")

	if err := view.Register(
		&view.View{
			Measure:     outsideTempMetric,
			Aggregation: view.LastValue()},
		&view.View{
			Measure:     roomTemp,
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{roomKey}},
		&view.View{
			Measure:     acState,
			Aggregation: view.LastValue(),
			TagKeys:     []tag.Key{roomKey}}); err != nil {
		log.Fatal(err)
	}

	exporter, err := stackdriver.NewExporter(stackdriver.Options{
		ProjectID:               os.Getenv("GOOGLE_PROJECT"),
		DefaultMonitoringLabels: &stackdriver.Labels{}, // remove default labels
		OnError: func(err error) {
			log.Printf("stackdriver exporter error: %v", err)
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	defer exporter.Flush()

	if err := exporter.StartMetricsExporter(); err != nil {
		log.Fatalf("error starting metric exporter: %v", err)
	}
	defer exporter.StopMetricsExporter()

	devices, err := GetDevices(apiKey)
	if err != nil {
		log.Fatal(err)
	}

	outsideTemp, outsideTempErr := getTemperature()
	if outsideTempErr != nil {
		log.Printf("warn: failed to get outside temperature: %v", outsideTempErr)
	} else {
		log.Println("outside_temp", outsideTemp)
		stats.Record(context.TODO(), outsideTempMetric.M(outsideTemp))
	}

	for _, d := range devices {
		roomName := sanitizeString(d.Room.Name)
		log.Println("recording "+d.ID, "room="+roomName,
			"temp="+fmt.Sprintf("%f", d.Measurements.Temperature),
			"ac="+fmt.Sprintf("%t", d.ACState.On))
		if err := stats.RecordWithTags(context.TODO(),
			[]tag.Mutator{tag.Upsert(roomKey, roomName)},
			roomTemp.M(d.Measurements.Temperature),
			acState.M(boolToInt(d.ACState.On)),
		); err != nil {
			log.Fatalf("failed to record measurement for device %s: %s", d.ID, err)
		}
	}
}

func getTemperature() (float64, error) {
	lat, lon := "47.68", "-122.38"
	url := fmt.Sprintf("https://api.open-meteo.com/v1/forecast?latitude=%s&longitude=%s&hourly=temperature_2m", lat, lon)
	resp, err := http.Get(url)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch weather: %w", err)
	}
	defer resp.Body.Close()
	type Response struct {
		Hourly struct {
			Temperature2m []float64 `json:"temperature_2m"`
		} `json:"hourly"`
	}
	var rv Response
	if err := json.NewDecoder(resp.Body).Decode(&rv); err != nil {
		return 0, fmt.Errorf("failed to decode weather response: %w", err)
	}
	if len(rv.Hourly.Temperature2m) == 0 {
		return 0, fmt.Errorf("no temperature data found")
	}
	return rv.Hourly.Temperature2m[0], nil
}

func boolToInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

func GetDevices(apiKey string) ([]DeviceInfo, error) {
	resp, err := http.Get("https://home.sensibo.com/api/v2/users/me/pods?apiKey=" + apiKey + "&fields=%2A")
	if err != nil {
		return nil, fmt.Errorf("request error: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := ioutil.ReadAll(resp.Body)
		return nil, fmt.Errorf("request failed code=%d error=%s", resp.StatusCode, string(body))
	}
	var out GetDevicesResponse
	err = json.NewDecoder(resp.Body).Decode(&out)
	return out.Result, err
}

type GetDevicesResponse struct {
	Result []DeviceInfo `json:"result"`
	Status string       `json:"status"`
}

type DeviceInfo struct {
	ID      string `json:"id"`
	ACState struct {
		On bool `json:"on"`
	} `json:"acState"`
	Room struct {
		Name string `json:"name"`
	} `json:"room"`
	Measurements struct {
		Temperature float64 `json:"temperature"`
	} `json:"measurements"`
}

// write a function to keep only the alpanumeric characters of a string
func sanitizeString(str string) string {
	var result string
	for _, char := range str {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') {
			result += string(char)
		} else if char == ' ' {
			result += "_"
		}
	}
	return result
}
