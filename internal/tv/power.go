package tv

import "time"

// Power is the last thing Home Assistant said about a TV.
type Power string

// Powers.
const (
	PowerUnknown Power = "unknown"
	PowerOn      Power = "on"
	PowerOff     Power = "off"
)

// StalePause is how long "idle" or "paused" may stay unchanged and still mean the TV is on. A
// streaming box left paused can stay awake after the screen goes dark, and Home Assistant can
// lose touch with it and keep its last state; past this the state is no evidence either way.
const StalePause = 15 * time.Minute

// PowerFromHomeAssistant reads a media_player state as TV power. ok is false for states that
// say nothing about the TV (unavailable, unknown, anything unrecognized): they change nothing.
// "idle" or "paused" unchanged for longer than StalePause is PowerUnknown, not off (old app
// #45), so router attempts decide again and an open session keeps running. lastChanged is the
// entity's last_changed; zero means not known, read as fresh.
func PowerFromHomeAssistant(state string, lastChanged, now time.Time) (p Power, ok bool) {
	switch state {
	case "off", "standby":
		return PowerOff, true
	case "idle", "paused":
		if !lastChanged.IsZero() && now.Sub(lastChanged) > StalePause {
			return PowerUnknown, true
		}
		return PowerOn, true
	case "on", "playing", "buffering":
		return PowerOn, true
	}
	return "", false
}
