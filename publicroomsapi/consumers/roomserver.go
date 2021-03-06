// Copyright 2017 Vector Creations Ltd
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package consumers

import (
	"context"
	"encoding/json"

	"github.com/Shopify/sarama"
	"github.com/matrix-org/dendrite/internal"
	"github.com/matrix-org/dendrite/internal/config"
	"github.com/matrix-org/dendrite/publicroomsapi/storage"
	"github.com/matrix-org/dendrite/roomserver/api"
	"github.com/matrix-org/gomatrixserverlib"
	log "github.com/sirupsen/logrus"
)

// OutputRoomEventConsumer consumes events that originated in the room server.
type OutputRoomEventConsumer struct {
	rsAPI      api.RoomserverInternalAPI
	rsConsumer *internal.ContinualConsumer
	db         storage.Database
}

// NewOutputRoomEventConsumer creates a new OutputRoomEventConsumer. Call Start() to begin consuming from room servers.
func NewOutputRoomEventConsumer(
	cfg *config.Dendrite,
	kafkaConsumer sarama.Consumer,
	store storage.Database,
	rsAPI api.RoomserverInternalAPI,
) *OutputRoomEventConsumer {
	consumer := internal.ContinualConsumer{
		Topic:          string(cfg.Kafka.Topics.OutputRoomEvent),
		Consumer:       kafkaConsumer,
		PartitionStore: store,
	}
	s := &OutputRoomEventConsumer{
		rsConsumer: &consumer,
		db:         store,
		rsAPI:      rsAPI,
	}
	consumer.ProcessMessage = s.onMessage

	return s
}

// Start consuming from room servers
func (s *OutputRoomEventConsumer) Start() error {
	return s.rsConsumer.Start()
}

// onMessage is called when the sync server receives a new event from the room server output log.
func (s *OutputRoomEventConsumer) onMessage(msg *sarama.ConsumerMessage) error {
	// Parse out the event JSON
	var output api.OutputEvent
	if err := json.Unmarshal(msg.Value, &output); err != nil {
		// If the message was invalid, log it and move on to the next message in the stream
		log.WithError(err).Errorf("roomserver output log: message parse failure")
		return nil
	}

	if output.Type != api.OutputTypeNewRoomEvent {
		log.WithField("type", output.Type).Debug(
			"roomserver output log: ignoring unknown output type",
		)
		return nil
	}

	ev := output.NewRoomEvent.Event
	log.WithFields(log.Fields{
		"event_id": ev.EventID(),
		"room_id":  ev.RoomID(),
		"type":     ev.Type(),
	}).Info("received event from roomserver")

	addQueryReq := api.QueryEventsByIDRequest{EventIDs: output.NewRoomEvent.AddsStateEventIDs}
	var addQueryRes api.QueryEventsByIDResponse
	if err := s.rsAPI.QueryEventsByID(context.TODO(), &addQueryReq, &addQueryRes); err != nil {
		log.Warn(err)
		return err
	}

	remQueryReq := api.QueryEventsByIDRequest{EventIDs: output.NewRoomEvent.RemovesStateEventIDs}
	var remQueryRes api.QueryEventsByIDResponse
	if err := s.rsAPI.QueryEventsByID(context.TODO(), &remQueryReq, &remQueryRes); err != nil {
		log.Warn(err)
		return err
	}

	var addQueryEvents, remQueryEvents []gomatrixserverlib.Event
	for _, headeredEvent := range addQueryRes.Events {
		addQueryEvents = append(addQueryEvents, headeredEvent.Event)
	}
	for _, headeredEvent := range remQueryRes.Events {
		remQueryEvents = append(remQueryEvents, headeredEvent.Event)
	}

	return s.db.UpdateRoomFromEvents(context.TODO(), addQueryEvents, remQueryEvents)
}
