package main

import (
	"gopkg.in/redis.v5"
	"log"
	"time"
	"encoding/json"
	"fmt"
	"github.com/aws/aws-sdk-go/aws/client"
)

type AuditLogger struct {
	redis_client   redis.Client
	events_channel chan map[string]interface{}
}

func NewAuditLogger(events chan map[string]interface{}) *AuditLogger {
	redis_client := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%d", config.AuditRedisHost, 6379),
		Password: "", // no password set
		DB:       config.AuditRedisDatabaseNumber,
	})

	return &AuditLogger{
		redis_client: *redis_client,
		events_channel: events,
	}
}

func (a *AuditLogger) run() {
	var err error
	go func() {
		events_pipelined := 0
		pipeline := a.redis_client.Pipeline()
		for {

			select {
			case event := <-a.events_channel:
				var b []byte
				b, err = json.Marshal(event)
				event_string := string(b)

				if err != nil {
					log.Println(err)
				} else {
					//log.Println("auditing an event")
					//log.Println(string(event_string))
					event_guid := event["event_guid"].(string)
					pipeline.HSet(event_guid, "ingest_ts", time.Now().String())
					pipeline.HSet(event_guid, "event_type", event["event_type"].(string))
					pipeline.HSet(event_guid, "raw_event", event_string)

					pipeline.ZAddNX("events_to_audit", redis.Z{float64(time.Now().Unix()), event_guid})
					pipeline.Expire(event_guid, 3600*time.Second)
					events_pipelined++
				}


			}

			if events_pipelined > config.AuditPipelineSize {
				log.Println("Flushing an auditing pipeline now")
				pipeline.Exec()
				pipeline.Close()
				pipeline = a.redis_client.Pipeline()
				events_pipelined = 0
			}
		}
	}()
}