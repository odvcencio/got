package entity

import "testing"

func BenchmarkExtractGoFile(b *testing.B) {
	source := []byte(`package main

import "fmt"

type Order struct {
	ID   string
	Qty  int
}

func ValidateOrder(o Order) error {
	if o.ID == "" {
		return fmt.Errorf("missing id")
	}
	if o.Qty <= 0 {
		return fmt.Errorf("qty must be positive")
	}
	return nil
}

func ProcessOrder(o Order) (string, error) {
	if err := ValidateOrder(o); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s:%d", o.ID, o.Qty), nil
}

func main() {
	_, _ = ProcessOrder(Order{ID: "abc", Qty: 1})
}
`)

	b.SetBytes(int64(len(source)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		entities, err := Extract("main.go", source)
		if err != nil {
			b.Fatalf("Extract: %v", err)
		}
		if len(entities.Entities) == 0 {
			b.Fatal("expected extracted entities")
		}
	}
}
