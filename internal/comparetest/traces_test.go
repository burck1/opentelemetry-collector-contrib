// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package comparetest

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/open-telemetry/opentelemetry-collector-contrib/internal/comparetest/golden"
)

func TestCompareTraces(t *testing.T) {
	tcs := []struct {
		name           string
		compareOptions []TracesCompareOption
		withoutOptions expectation
		withOptions    expectation
	}{
		{
			name: "equal",
		},
	}

	for _, tc := range tcs {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join("testdata", "traces", tc.name)

			expected, err := golden.ReadTraces(filepath.Join(dir, "expected.json"))
			require.NoError(t, err)

			actual, err := golden.ReadTraces(filepath.Join(dir, "actual.json"))
			require.NoError(t, err)

			err = CompareTraces(expected, actual)
			tc.withoutOptions.validate(t, err)

			if tc.compareOptions == nil {
				return
			}

			err = CompareTraces(expected, actual, tc.compareOptions...)
			tc.withOptions.validate(t, err)
		})
	}
}
