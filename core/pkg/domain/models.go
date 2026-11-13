package domain

import (
	"time"
)

// Provider representa una plataforma de mensajería integrada.
type Provider string

const (
	ProviderWhatsApp Provider = "whatsapp"
	ProviderTelegram Provider = "telegram"
	ProviderDiscord  Provider = "discord"
	ProviderSlack    Provider = "slack"
	ProviderGmail    Provider = "gmail"
)

// Contact representa una identidad humana unificada.
type Contact struct {
	ID          string           `json:"id"`
	DisplayName string           `json:"display_name"`
	AvatarURL   string           `json:"avatar_url,omitempty"`
	Email       string           `json:"email,omitempty"` // Identidad canónica normalizada (minúsculas)
	Phone       string           `json:"phone,omitempty"` // Identidad canónica normalizada (E.164)
	Channels    []ContactChannel `json:"channels,omitempty"`
	CreatedAt   time.Time        `json:"created_at"`
	UpdatedAt   time.Time        `json:"updated_at"`
}

// ContactChannel representa un punto de contacto en una plataforma concreta.
type ContactChannel struct {
	ID              string   `json:"id"`
	ContactID       string   `json:"contact_id"`
	Provider        Provider `json:"provider"`
	RemoteID        string   `json:"remote_id"` // E.164 phone, TG user_id, Discord snowflake, email
	Handle          string   `json:"handle,omitempty"`
	IsPreferred     bool     `json:"is_preferred"`
	ServerID        string   `json:"server_id,omitempty"`
	ParentChannelID string   `json:"parent_channel_id,omitempty"`
	ChannelType     string   `json:"channel_type,omitempty"`
	Metadata        string   `json:"metadata,omitempty"` // JSON string
}

// Conversation representa un hilo o chat (1 a 1, grupo, o canal de servidor).
type Conversation struct {
	ID              string    `json:"id"`
	ContactID       *string   `json:"contact_id,omitempty"` // NULL si es grupo no vinculado
	IsGroup         bool      `json:"is_group"`
	Title           string    `json:"title"`
	LastMessageAt   time.Time `json:"last_message_at"`
	ActiveChannelID *string   `json:"active_channel_id,omitempty"` // Canal activo (sticky)
	ServerID        string    `json:"server_id,omitempty"`
	ServerName      string    `json:"server_name,omitempty"`
	ParentChannelID string    `json:"parent_channel_id,omitempty"`
	ChannelType     string    `json:"channel_type,omitempty"`
	AvatarURL       string    `json:"avatar_url,omitempty"`
	Position        int       `json:"position,omitempty"` // Orden en que el servidor publica el canal
}

// SenderType define el origen del mensaje.
type SenderType string

const (
	SenderMe      SenderType = "me"
	SenderContact SenderType = "contact"
	SenderBot     SenderType = "bot"
	SenderSystem  SenderType = "system"
)

// ContentType define el tipo de contenido del mensaje.
type ContentType string

const (
	ContentTypeText     ContentType = "text"
	ContentTypeImage    ContentType = "image"
	ContentTypeAudio    ContentType = "audio"
	ContentTypeVideo    ContentType = "video"
	ContentTypeDocument ContentType = "document"
	ContentTypeSystem   ContentType = "system"
	ContentTypePoll     ContentType = "poll"
)

// MessageStatus define el ciclo de vida de entrega de un mensaje.
type MessageStatus string

const (
	StatusPending   MessageStatus = "pending"
	StatusSent      MessageStatus = "sent"
	StatusDelivered MessageStatus = "delivered"
	StatusRead      MessageStatus = "read"
	StatusFailed    MessageStatus = "failed"
)

// Message representa un mensaje normalizado independientemente del proveedor.
type Message struct {
	ID               string         `json:"id"`
	ConversationID   string         `json:"conversation_id"`
	ChannelID        string         `json:"channel_id"`
	RemoteMessageID  string         `json:"remote_message_id"`
	SenderType       SenderType     `json:"sender_type"`
	SenderRemoteID   string         `json:"sender_remote_id,omitempty"` // Remitente remoto (participante en grupos)
	Body             string         `json:"body"`
	ContentType      ContentType    `json:"content_type"`
	Status           MessageStatus  `json:"status"`
	SentAt           time.Time      `json:"sent_at"`
	ReceivedAt       time.Time      `json:"received_at"`
	ReplyToMessageID *string        `json:"reply_to_message_id,omitempty"`
	ReplyToSender    string         `json:"reply_to_sender,omitempty"`
	ReplyToBody      string         `json:"reply_to_body,omitempty"`
	Reactions        []Reaction     `json:"reactions,omitempty"`
	Poll             *Poll          `json:"poll,omitempty"`
	ButtonRows       []ButtonRow    `json:"button_rows,omitempty"`
	IsEdited         bool           `json:"is_edited"`
	EditedAt         *time.Time     `json:"edited_at,omitempty"`
	IsPinned         bool           `json:"is_pinned"`
	Location         *Location      `json:"location,omitempty"`
	SharedContact    *SharedContact `json:"shared_contact,omitempty"`
	Metadata         string         `json:"metadata,omitempty"` // JSON string
	Attachments      []Attachment   `json:"attachments,omitempty"`
}

// Location representa una geolocalización compartida.
type Location struct {
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
	Title     string  `json:"title,omitempty"`
	Address   string  `json:"address,omitempty"`
}

// SharedContact representa un contacto o vCard compartido.
type SharedContact struct {
	DisplayName string `json:"display_name"`
	PhoneNumber string `json:"phone_number"`
	Vcard       string `json:"vcard,omitempty"`
}

// Button representa un botón interactivo adjunto a un mensaje.
type Button struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	Type string `json:"type"` // "callback", "url", "quick_reply"
	URL  string `json:"url,omitempty"`
}

// ButtonRow representa una fila de botones interactivos.
type ButtonRow struct {
	Buttons []Button `json:"buttons"`
}

// PollOption representa una opción dentro de una encuesta.
type PollOption struct {
	ID        string   `json:"id"`
	Text      string   `json:"text"`
	VoteCount int      `json:"vote_count"`
	Voters    []string `json:"voters,omitempty"`
	Chosen    bool     `json:"chosen"`
}

// Poll representa una encuesta unificada.
type Poll struct {
	Question        string       `json:"question"`
	Options         []PollOption `json:"options"`
	SelectableCount int          `json:"selectable_count"` // 1 = opción única, >1 = múltiple
	IsClosed        bool         `json:"is_closed"`
	TotalVotes      int          `json:"total_votes"`
	IsQuiz          bool         `json:"is_quiz"`
}

// Reaction representa una reacción con emoji agregada a un mensaje.
type Reaction struct {
	Emoji      string   `json:"emoji"`
	Count      int      `json:"count"`
	Senders    []string `json:"senders,omitempty"`
	IncludesMe bool     `json:"includes_me"`
}

// DownloadStatus define el estado de descarga de un archivo multimedia.
type DownloadStatus string

const (
	MediaRemoteOnly  DownloadStatus = "remote_only"
	MediaDownloading DownloadStatus = "downloading"
	MediaCached      DownloadStatus = "cached"
	MediaFailed      DownloadStatus = "failed"
)

// Attachment representa un adjunto o archivo multimedia.
type Attachment struct {
	ID             string         `json:"id"`
	MessageID      string         `json:"message_id"`
	FileName       string         `json:"file_name"`
	MimeType       string         `json:"mime_type"`
	FileSize       int64          `json:"file_size"`
	LocalPath      *string        `json:"local_path,omitempty"`
	DownloadStatus DownloadStatus `json:"download_status"`
	Checksum       string         `json:"checksum,omitempty"`
	RemoteRef      string         `json:"-"` // receta opaca del plugin; nunca se expone a clientes
	ExpiresAt      *time.Time     `json:"expires_at,omitempty"`
}
