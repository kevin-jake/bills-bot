package storage

import "time"

// The models below are the rows as SQLite holds them, not the household's vocabulary:
// channels are strings, absent values are pointers, and nothing here knows a rule. The
// domain types they map to live in internal/domain.

// Section is a row of the sections table.
type Section struct {
	ID           int64 `gorm:"primaryKey"`
	Name         string
	DisplayOrder int
	CreatedAt    time.Time
}

// TableName pins the table, so a rename of the Go type cannot silently repoint it.
func (Section) TableName() string { return "sections" }

// Bill is a row of the bills table.
type Bill struct {
	ID   int64 `gorm:"primaryKey"`
	Name string
	// Aliases is a comma-separated list, kept flat because the fuzzy matcher reads all of
	// a Bill's names at once and nothing ever queries one alias on its own.
	Aliases   string
	SectionID int64
	Channel   string
	// CardName is a pointer because the schema's CHECK compares it against NULL: an empty
	// string would be a card name as far as SQLite is concerned, and the row would be
	// rejected on every channel.
	CardName *string
	// CardLast4 and DueDay are pointers for the same reason: NULL is "this bill has none",
	// which an empty string or a zero day would quietly turn into a real value.
	CardLast4    *string
	DueDay       *int
	DisplayOrder int
	ArchivedAt   *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// TableName pins the table.
func (Bill) TableName() string { return "bills" }

// Archived reports whether the Bill has been taken off the standing list.
func (b Bill) Archived() bool { return b.ArchivedAt != nil }

// Event is one row of the audit trail. Every mutation appends exactly one, which is what
// makes Undo and the per-Bill history possible.
type Event struct {
	ID         int64     `gorm:"primaryKey"`
	OccurredAt time.Time `gorm:"column:occurred_at"`
	// ActorTelegramID is 0 for the scheduler, which acts as nobody in particular.
	ActorTelegramID int64  `gorm:"column:actor_telegram_id"`
	ActorName       string `gorm:"column:actor_name"`
	Action          string
	CycleID         *int64
	BillID          *int64
	PayableID       *int64
	TransferID      *int64
	BeforeJSON      *string `gorm:"column:before_json"`
	AfterJSON       *string `gorm:"column:after_json"`
	Note            *string
}

// TableName pins the table.
func (Event) TableName() string { return "events" }

// Cycle is a row of the cycles table.
type Cycle struct {
	ID int64 `gorm:"primaryKey"`
	// Month is "YYYY-MM" in Manila.
	Month    string
	OpenedAt time.Time
	ClosedAt *time.Time
	// BoardChatID and BoardMessageID stay NULL until a Board has been posted, which is how
	// the scheduler will tell an opened Cycle from one whose Board actually reached the group.
	BoardChatID    *int64
	BoardMessageID *int
}

// TableName pins the table.
func (Cycle) TableName() string { return "cycles" }

// Payable is a row of the payables table.
type Payable struct {
	ID      int64 `gorm:"primaryKey"`
	CycleID int64
	BillID  int64
	// AmountCents is NULL while the amount is unknown, which is not the same as zero.
	AmountCents *int64
	Status      string
	Channel     string
	CardName    *string
	PaidAt      *time.Time
	PaidBy      *int64
	UpdatedAt   time.Time
}

// TableName pins the table.
func (Payable) TableName() string { return "payables" }

// Transfer is a row of the transfers table.
type Transfer struct {
	ID        int64 `gorm:"primaryKey"`
	CycleID   int64
	Channel   string
	SentCents int64
	SentAt    time.Time
	SentBy    int64
}

// TableName pins the table.
func (Transfer) TableName() string { return "transfers" }
