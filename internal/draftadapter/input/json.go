package input

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	draftmodel "github.com/tyler180/dynasty-ff-models/draft"
)

func Read(path string) (draftmodel.Input, error) {
	var reader io.Reader = os.Stdin
	if path != "" && path != "-" {
		file, err := os.Open(path)
		if err != nil {
			return draftmodel.Input{}, fmt.Errorf("open input: %w", err)
		}
		defer file.Close()
		reader = file
	}

	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	var in draftmodel.Input
	if err := decoder.Decode(&in); err != nil {
		return draftmodel.Input{}, fmt.Errorf("decode input: %w", err)
	}
	in.ApplyDefaults()
	if err := in.Validate(); err != nil {
		return draftmodel.Input{}, fmt.Errorf("validate input: %w", err)
	}
	return in, nil
}
