// Package config owns the master configuration file and the render
// pipeline: master config + subscription providers + user rules are
// rendered into the effective mihomo config, validated with `mihomo -t`,
// swapped in atomically and hot-reloaded; a failed validation keeps the
// last known-good config in place.
package config
