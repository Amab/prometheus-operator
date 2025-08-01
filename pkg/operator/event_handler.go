// Copyright 2023 The prometheus-operator Authors
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

package operator

import (
	"fmt"
	"log/slog"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
)

type FilterFunc func(interface{}) bool

type EventHandlerOption func(*EventHandler)

// The object is enqueued only if FilterFunc returns true.
func WithFilter(filter FilterFunc) EventHandlerOption {
	return func(eh *EventHandler) {
		eh.filterFuncs = append(eh.filterFuncs, filter)
	}
}

// EventHandler implements the k8s.io/tools/cache.ResourceEventHandler interface.
type EventHandler struct {
	logger   *slog.Logger
	accessor *Accessor
	metrics  *Metrics

	objName     string
	enqueueFunc func(string)
	filterFuncs []FilterFunc
}

var _ = cache.ResourceEventHandler(&EventHandler{})

func NewEventHandler(
	logger *slog.Logger,
	accessor *Accessor,
	metrics *Metrics,
	objName string,
	enqueueFunc func(ns string),
	options ...EventHandlerOption,
) *EventHandler {
	eh := &EventHandler{
		logger:      logger,
		accessor:    accessor,
		metrics:     metrics,
		objName:     objName,
		enqueueFunc: enqueueFunc,
	}

	for _, opt := range options {
		opt(eh)
	}

	return eh
}

func (e *EventHandler) OnAdd(obj interface{}, _ bool) {
	o, ok := e.accessor.ObjectMetadata(obj)
	if !ok {
		return
	}

	for _, fn := range e.filterFuncs {
		if !fn(obj) {
			return
		}
	}

	e.recordEvent(AddEvent, o)
	e.enqueueFunc(o.GetNamespace())
}

func (e *EventHandler) OnUpdate(old, cur interface{}) {
	if old.(metav1.Object).GetResourceVersion() == cur.(metav1.Object).GetResourceVersion() {
		return
	}

	for _, fn := range e.filterFuncs {
		if !fn(cur) {
			return
		}
	}

	if o, ok := e.accessor.ObjectMetadata(cur); ok {
		e.recordEvent(UpdateEvent, o)
		e.enqueueFunc(o.GetNamespace())
	}
}

func (e *EventHandler) OnDelete(obj interface{}) {
	o, ok := e.accessor.ObjectMetadata(obj)
	if !ok {
		return
	}

	for _, fn := range e.filterFuncs {
		if !fn(obj) {
			return
		}
	}

	e.recordEvent(DeleteEvent, o)
	e.enqueueFunc(o.GetNamespace())
}

func (e *EventHandler) recordEvent(event HandlerEvent, obj metav1.Object) {
	eventName := string(event)
	if strings.HasSuffix(eventName, "e") {
		eventName += "d"
	} else {
		eventName += "ed"
	}

	e.logger.Debug(
		fmt.Sprintf("%s %s", e.objName, eventName),
		strings.ToLower(e.objName), fmt.Sprintf("%s/%s", obj.GetNamespace(), obj.GetName()),
	)
	e.metrics.TriggerByCounter(e.objName, event).Inc()
}
