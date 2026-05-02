package main

import "time"

type pollingGroup string

const (
	pollHeartbeat     pollingGroup = "heartbeat"
	pollRuntime       pollingGroup = "runtime"
	pollRuntimeUpload pollingGroup = "runtime_upload"
	pollUsage         pollingGroup = "usage"
	pollIPPoll        pollingGroup = "ip_poll"
	pollIPUpload      pollingGroup = "ip_upload"
)

type pollingGroupConfig struct {
	Enabled  bool
	Interval time.Duration
}

type connectionSchedule struct {
	groups map[pollingGroup]pollingGroupConfig
}

func newConnectionSchedule(heartbeat, runtimePoll, runtimeUpload, usageSnapshot, ipPoll, ipUpload time.Duration) connectionSchedule {
	return connectionSchedule{
		groups: map[pollingGroup]pollingGroupConfig{
			pollHeartbeat:     {Enabled: heartbeat > 0, Interval: heartbeat},
			pollRuntime:       {Enabled: runtimePoll > 0, Interval: runtimePoll},
			pollRuntimeUpload: {Enabled: runtimeUpload > 0, Interval: runtimeUpload},
			pollUsage:         {Enabled: usageSnapshot > 0, Interval: usageSnapshot},
			pollIPPoll:        {Enabled: ipPoll > 0, Interval: ipPoll},
			pollIPUpload:      {Enabled: ipUpload > 0, Interval: ipUpload},
		},
	}
}

func (s connectionSchedule) config(group pollingGroup) pollingGroupConfig {
	return s.groups[group]
}

func newTicker(config pollingGroupConfig) *time.Ticker {
	if !config.Enabled || config.Interval <= 0 {
		return nil
	}
	return time.NewTicker(config.Interval)
}

func tickerChan(ticker *time.Ticker) <-chan time.Time {
	if ticker == nil {
		return nil
	}
	return ticker.C
}

func timerChan(timer *time.Timer) <-chan time.Time {
	if timer == nil {
		return nil
	}
	return timer.C
}
