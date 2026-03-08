package parser

import (
	"github.com/nelfander/losu/internal/model"
)

// Parser defines how to turn a raw line into a structured event
type Parser interface {
	Parse(raw model.RawLog) model.LogEvent
}
