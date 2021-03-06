package alerts

import (
	"fmt"
	"strconv"

	"go.skia.org/infra/perf/go/clustering2"
)

const (
	INVALID_ID = -1
)

// Config represents the configuration for one alert.
type Config struct {
	ID             int                     `json:"id"`
	Query          string                  `json:"query"`            // The query to perform on the trace store to select the traces to alert on.
	Alert          string                  `json:"alert"`            // Email address or id of a chat room to send alerts to.
	Interesting    float32                 `json:"interesting"`      // The regression interestingness threshhold.
	BugURITemplate string                  `json:"bug_uri_template"` // URI Template used for reporting bugs. Format TBD.
	Algo           clustering2.ClusterAlgo `json:"algo"`             // Which clustering algorithm to use.
	State          ConfigState             `json:"state"`            // The state of the config.
	Owner          string                  `json:"owner"`            // Email address of the person that owns this alert.
	StepUpOnly     bool                    `json:"step_up_only"`     // If true then only steps up will trigger an alert.
	Radius         int                     `json:"radius"`           // How many commits to each side of a commit to consider when looking for a step. 0 means use the server default.
	K              int                     `json:"k"`                // The K in k-means clustering. 0 means use an algorithmically chosen value based on the data.
}

func (c *Config) IdAsString() string {
	return fmt.Sprintf("%d", c.ID)
}

func (c *Config) StringToId(s string) {
	if i, err := strconv.ParseInt(s, 10, 32); err != nil {
		c.ID = -1
	} else {
		c.ID = int(i)
	}
}

// NewConfig creates a new Config properly initialized.
func NewConfig() *Config {
	return &Config{
		ID:    INVALID_ID,
		Algo:  clustering2.KMEANS_ALGO,
		State: ACTIVE,
	}
}
