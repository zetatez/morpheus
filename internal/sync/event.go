package sync

import (
	"encoding/json"
	"fmt"
	"sync"
)

type Definition struct {
	Type      string
	Version   int
	Aggregate string
	Schema    SchemaValidator
}

type SchemaValidator interface {
	Validate(data any) error
}

type BaseSchema struct{}

func (s BaseSchema) Validate(data any) error {
	return nil
}

type JSONSchema struct {
	data map[string]any
}

func NewJSONSchema(data map[string]any) *JSONSchema {
	return &JSONSchema{data: data}
}

func (s *JSONSchema) Validate(data any) error {
	_, err := json.Marshal(data)
	return err
}

type Event struct {
	ID          string
	Seq         int
	AggregateID string
	Type        string
	Data        any
}

type SerializedEvent struct {
	ID          string          `json:"id"`
	Seq         int             `json:"seq"`
	AggregateID string          `json:"aggregateID"`
	Type        string          `json:"type"`
	Data        json.RawMessage `json:"data"`
}

type ProjectorFunc func(tx Tx, data any) error

var (
	registry   = make(map[string]*Definition)
	projectors = make(map[*Definition]ProjectorFunc)
	versions   = make(map[string]int)
	frozen     = false
	mu         sync.RWMutex
	EventBus   = &EventEmitter{}
)

type EventEmitter struct {
	mu       sync.RWMutex
	handlers []func(def *Definition, event *Event)
}

func (e *EventEmitter) On(handler func(def *Definition, event *Event)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.handlers = append(e.handlers, handler)
}

func (e *EventEmitter) Off(handler func(def *Definition, event *Event)) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for i, h := range e.handlers {
		if &h == &handler {
			e.handlers = append(e.handlers[:i], e.handlers[i+1:]...)
			break
		}
	}
}

func (e *EventEmitter) Emit(def *Definition, event *Event) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	for _, handler := range e.handlers {
		handler(def, event)
	}
}

func VersionedType(typeName string, version int) string {
	return fmt.Sprintf("%s/%d", typeName, version)
}

func Define(input struct {
	Type      string
	Version   int
	Aggregate string
	Schema    SchemaValidator
}) *Definition {
	mu.Lock()
	defer mu.Unlock()

	if frozen {
		panic("cannot define event after sync system has been frozen")
	}

	def := &Definition{
		Type:      input.Type,
		Version:   input.Version,
		Aggregate: input.Aggregate,
		Schema:    input.Schema,
	}

	currentVersion := versions[input.Type]
	if input.Version > currentVersion {
		versions[input.Type] = input.Version
	}

	key := VersionedType(input.Type, input.Version)
	registry[key] = def

	return def
}

func Project(def *Definition, fn func(tx Tx, data any) error) ProjectorFunc {
	mu.Lock()
	defer mu.Unlock()
	projectors[def] = fn
	return fn
}

func Init(projectors_ []struct {
	Def  *Definition
	Func ProjectorFunc
}) {
	mu.Lock()
	defer mu.Unlock()

	for _, p := range projectors_ {
		projectors[p.Def] = p.Func
	}

	frozen = true
}

func Reset() {
	mu.Lock()
	defer mu.Unlock()
	frozen = false
	projectors = make(map[*Definition]ProjectorFunc)
}

func GetDefinition(eventType string, version int) (*Definition, bool) {
	mu.RLock()
	defer mu.RUnlock()
	key := VersionedType(eventType, version)
	def, ok := registry[key]
	return def, ok
}

func GetLatestDefinition(eventType string) (*Definition, bool) {
	mu.RLock()
	defer mu.RUnlock()
	version, ok := versions[eventType]
	if !ok {
		return nil, false
	}
	key := VersionedType(eventType, version)
	def, ok := registry[key]
	return def, ok
}

func Run(def *Definition, aggregateID string, data any) error {
	mu.RLock()
	currentVersion := versions[def.Type]
	mu.RUnlock()

	if def.Version != currentVersion {
		return fmt.Errorf("running old versions of events is not allowed: %s", def.Type)
	}

	if err := def.Schema.Validate(data); err != nil {
		return fmt.Errorf("schema validation failed: %w", err)
	}

	projector, ok := projectors[def]
	if !ok {
		return fmt.Errorf("projector not found for event: %s", def.Type)
	}

	_ = projector

	return nil
}

func Subscribe(handler func(def *Definition, event *Event)) func() {
	EventBus.On(handler)
	return func() {
		EventBus.Off(handler)
	}
}

type Tx interface {
	Exec(query string, args ...any) (Result, error)
	Query(query string, args ...any) (Rows, error)
}

type Result interface {
	LastInsertId() (int64, error)
	RowsAffected() (int64, error)
}

type Rows interface {
	Close() error
	Next() bool
	Scan(dest ...any) error
}
