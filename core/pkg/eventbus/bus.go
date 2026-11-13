package eventbus

import (
	"sync"
	"time"
)

// EventType define el nombre del evento.
type EventType string

const (
	EventMessageReceived  EventType = "message.received"
	EventMessageSent      EventType = "message.sent"
	EventMessageUpdated   EventType = "message.updated"
	EventMessageDeleted   EventType = "message.deleted"
	EventMessageStatus    EventType = "message.status_updated"
	EventContactUpdated   EventType = "contact.updated"
	EventChannelConnected EventType = "channel.connected"
	EventChannelError     EventType = "channel.error"
	EventSyncProgress     EventType = "sync.progress"
	EventConversationRead EventType = "conversation.read"
)

// Event representa la envoltura de cualquier evento en el bus.
type Event struct {
	Type      EventType   `json:"type"`
	Timestamp time.Time   `json:"timestamp"`
	Payload   interface{} `json:"payload"`
}

// Subscription canal tipado para recibir eventos.
type Subscription chan Event

// Bus representa un bus pub/sub en memoria, seguro para concurrencia.
type Bus struct {
	mu          sync.RWMutex
	subscribers map[EventType][]Subscription
	closed      bool
}

// NewBus inicializa el event bus.
func NewBus() *Bus {
	return &Bus{
		subscribers: make(map[EventType][]Subscription),
	}
}

// Subscribe se suscribe a un tipo de evento específico.
func (b *Bus) Subscribe(topic EventType) Subscription {
	b.mu.Lock()
	defer b.mu.Unlock()

	ch := make(Subscription, 100) // Buffer para evitar bloquear el bus
	b.subscribers[topic] = append(b.subscribers[topic], ch)
	return ch
}

// Publish emite un evento a todos los suscriptores registrados.
func (b *Bus) Publish(topic EventType, payload interface{}) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.closed {
		return
	}

	event := Event{
		Type:      topic,
		Timestamp: time.Now(),
		Payload:   payload,
	}

	for _, sub := range b.subscribers[topic] {
		select {
		case sub <- event:
		default:
			// Si el buffer está lleno, no bloqueamos el emisor
		}
	}
}

// Unsubscribe elimina un canal de suscripción.
func (b *Bus) Unsubscribe(topic EventType, ch Subscription) {
	b.mu.Lock()
	defer b.mu.Unlock()

	subs := b.subscribers[topic]
	for i, sub := range subs {
		if sub == ch {
			b.subscribers[topic] = append(subs[:i], subs[i+1:]...)
			close(ch)
			break
		}
	}
}

// Close cierra el bus y todos los canales de suscripción.
func (b *Bus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}
	b.closed = true

	for _, subs := range b.subscribers {
		for _, sub := range subs {
			close(sub)
		}
	}
	b.subscribers = make(map[EventType][]Subscription)
}
