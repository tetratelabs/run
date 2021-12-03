// Copyright (c) Tetrate, Inc 2021.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package log

import (
	"context"
	"log"
	"time"

	"github.com/tetratelabs/telemetry"
)

// Logger holds a very bare bones minimal implementation of telemetry.Logging.
// It is used by run.Group when not wired up with an explicit Logging
// implementation.
type Logger struct {
	args   []interface{}
}


func (l *Logger) Debug(msg string, keyValuePairs ...interface{}) {
	args := []interface{}{
		time.Now().Format("2006-01-02 15:04:05.000000  "),
		"msg", msg, "level", "debug",
	}
	args = append(args, keyValuePairs...)
	log.Println(args...)
}

func (l *Logger) Info(msg string, keyValuePairs ...interface{}) {
	args := []interface{}{
		time.Now().Format("2006-01-02 15:04:05.000000  "),
		"msg", msg, "level", "info",
	}
	args = append(args, keyValuePairs...)
	log.Println(args...)
}

func (l *Logger) Error(msg string, err error, keyValuePairs ...interface{}) {
	args := []interface{}{
		time.Now().Format("2006-01-02 15:04:05.000000  "),
		"msg", msg, "level", "error", "error", err.Error(),
	}
	args = append(args, keyValuePairs...)
	log.Println(args...)
}

func (l *Logger) With(_ ...interface{}) telemetry.Logger {
	// not used by run.Group
	return l
}

func (l *Logger) KeyValuesToContext(ctx context.Context, _ ...interface{}) context.Context {
	// not used by run.Group
	return ctx
}

func (l *Logger) Context(_ context.Context) telemetry.Logger {
	// not used by run.Group
	return l
}

func (l *Logger) Metric(_ telemetry.Metric) telemetry.Logger {
	// not used by run.Group
	return l
}

var _ telemetry.Logger = (*Logger)(nil)
