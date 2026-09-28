package model

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestScenarioFilesMatchBrowserExamples(t *testing.T) {
	for name, file := range map[string]string{
		"barrier": "barrier.json", "eager": "eager.json",
		"partition": "partition-heal.json", "lost-result": "lost-result.json",
	} {
		t.Run(name, func(t *testing.T) {
			f, err := os.Open(filepath.Join("..", "..", "examples", file))
			if err != nil {
				t.Fatal(err)
			}
			var scenario Scenario
			err = Decode(f, &scenario)
			closeErr := f.Close()
			if err != nil || closeErr != nil {
				t.Fatal(err, closeErr)
			}
			if !reflect.DeepEqual(scenario, Examples()[name]) {
				t.Fatal("checked-in scenario differs from browser example")
			}
		})
	}
}
