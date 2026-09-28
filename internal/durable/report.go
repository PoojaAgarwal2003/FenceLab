package durable

import "fmt"

type CrashCase struct {
	Operation string   `json:"operation"`
	Point     string   `json:"point"`
	Recovery  Recovery `json:"recovery"`
	Recovered State    `json:"recovered"`
	NextToken int      `json:"next_token"`
	Retry     Outcome  `json:"retry"`
	Safe      bool     `json:"safe"`
}

type CrashReport struct {
	Version string      `json:"version"`
	Scope   string      `json:"scope"`
	Cases   []CrashCase `json:"cases"`
	Safe    bool        `json:"safe"`
}

// CheckCrashReport checks internal consistency, not execution provenance.
func CheckCrashReport(r CrashReport) error {
	if r.Version != "fencelab/durability-v1" || !r.Safe || len(r.Cases) != 15 {
		return fmt.Errorf("expected a successful 15-case durability report")
	}
	seen := map[string]bool{}
	for _, c := range r.Cases {
		id := c.Operation + "/" + c.Point
		if seen[id] || !c.Safe || (c.Operation != "reserve" && c.Operation != "fence" && c.Operation != "write") {
			return fmt.Errorf("invalid or duplicated crash case %s", id)
		}
		seen[id] = true
		applied := int(c.Recovered.Sequence) - 2
		if applied < 0 || applied > 1 || c.Recovery.Records != c.Recovered.Sequence {
			return fmt.Errorf("invalid recovered sequence in %s", id)
		}
		truncated := int64(0)
		switch c.Point {
		case "before-append":
			if applied != 0 {
				return fmt.Errorf("pre-append operation survived in %s", id)
			}
		case "after-header":
			truncated = headerSize
			if applied != 0 {
				return fmt.Errorf("header-only operation survived in %s", id)
			}
		case "after-append":
		case "after-sync", "after-apply":
			if applied != 1 {
				return fmt.Errorf("synced operation lost in %s", id)
			}
		default:
			return fmt.Errorf("unknown crash checkpoint")
		}
		epoch, fence, effects := 1, 1, 0
		switch c.Operation {
		case "reserve":
			epoch += applied
		case "fence":
			fence += applied
		case "write":
			effects = applied
		}
		if c.Recovery.TruncatedBytes != truncated || c.Recovered.Epoch != epoch ||
			c.Recovered.Fence != fence || len(c.Recovered.Effects) != effects || c.NextToken != epoch+1 {
			return fmt.Errorf("recovered state differs from crash boundary in %s", id)
		}
		expected := Outcome{Status: "committed", Effect: Effect{Sequence: c.Recovered.Sequence + 2, Token: epoch + 1, Key: "invoice-001"}}
		if effects == 1 {
			effect := Effect{Sequence: 3, Token: 1, Key: "invoice-001"}
			if c.Recovered.Effects[0] != effect {
				return fmt.Errorf("wrong recovered effect in %s", id)
			}
			expected = Outcome{Status: "deduplicated", Effect: effect}
		}
		if c.Retry != expected {
			return fmt.Errorf("retry did not preserve the logical effect in %s", id)
		}
	}
	return nil
}
