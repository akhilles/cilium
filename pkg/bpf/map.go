// Copyright 2016-2019 Authors of Cilium
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

package bpf

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"time"

	"github.com/cilium/cilium/pkg/controller"
	"github.com/cilium/cilium/pkg/logging/logfields"

	"github.com/sirupsen/logrus"
)

// MapType is an enumeration for valid BPF map types
type MapType int

// This enumeration must be in sync with enum bpf_prog_type in <linux/bpf.h>
const (
	MapTypeUnspec MapType = iota
	MapTypeHash
	MapTypeArray
	MapTypeProgArray
	MapTypePerfEventArray
	MapTypePerCPUHash
	MapTypePerCPUArray
	MapTypeStackTrace
	MapTypeCgroupArray
	MapTypeLRUHash
	MapTypeLRUPerCPUHash
	MapTypeLPMTrie
	MapTypeArrayOfMaps
	MapTypeHashOfMaps
	MapTypeDevMap
	MapTypeSockMap
	MapTypeCPUMap
	MapTypeXSKMap
	MapTypeSockHash
	// MapTypeMaximum is the maximum supported known map type.
	MapTypeMaximum

	// maxSyncErrors is the maximum consecutive errors syncing before the
	// controller bails out
	maxSyncErrors = 512

	// errorResolverSchedulerMinInterval is the minimum interval for the
	// error resolver to be scheduled. This minimum interval ensures not to
	// overschedule if a large number of updates fail in a row.
	errorResolverSchedulerMinInterval = 5 * time.Second

	// errorResolverSchedulerDelay is the delay to update the controller
	// after determination that a run is needed. The delay allows to
	// schedule the resolver after series of updates have failed.
	errorResolverSchedulerDelay = 200 * time.Millisecond
)

var (
	mapControllers = controller.NewManager()

	// supportedMapTypes maps from a MapType to a bool indicating whether
	// the currently running kernel supports the map type.
	supportedMapTypes = make(map[MapType]bool)
)

func (t MapType) String() string {
	switch t {
	case MapTypeHash:
		return "Hash"
	case MapTypeArray:
		return "Array"
	case MapTypeProgArray:
		return "Program array"
	case MapTypePerfEventArray:
		return "Event array"
	case MapTypePerCPUHash:
		return "Per-CPU hash"
	case MapTypePerCPUArray:
		return "Per-CPU array"
	case MapTypeStackTrace:
		return "Stack trace"
	case MapTypeCgroupArray:
		return "Cgroup array"
	case MapTypeLRUHash:
		return "LRU hash"
	case MapTypeLRUPerCPUHash:
		return "LRU per-CPU hash"
	case MapTypeLPMTrie:
		return "Longest prefix match trie"
	case MapTypeArrayOfMaps:
		return "Array of maps"
	case MapTypeHashOfMaps:
		return "Hash of maps"
	case MapTypeDevMap:
		return "Device Map"
	case MapTypeSockMap:
		return "Socket Map"
	case MapTypeCPUMap:
		return "CPU Redirect Map"
	case MapTypeSockHash:
		return "Socket Hash"
	}

	return "Unknown"
}

func (t MapType) allowsPreallocation() bool {
	if t == MapTypeLPMTrie {
		return false
	}
	return true
}

func (t MapType) requiresPreallocation() bool {
	switch t {
	case MapTypeHash, MapTypePerCPUHash, MapTypeLPMTrie, MapTypeHashOfMaps:
		return false
	}
	return true
}

// DesiredAction is the action to be performed on the BPF map
type DesiredAction int

const (
	// OK indicates that to further action is required and the entry is in
	// sync
	OK DesiredAction = iota

	// Insert indicates that the entry needs to be created or updated
	Insert

	// Delete indicates that the entry needs to be deleted
	Delete
)

func (d DesiredAction) String() string {
	switch d {
	case OK:
		return "sync"
	case Insert:
		return "to-be-inserted"
	case Delete:
		return "to-be-deleted"
	default:
		return "unknown"
	}
}

// mapTypeToFeatureString maps a MapType into a string defined by run_probes.sh
func mapTypeToFeatureString(mt MapType) string {
	var featureString string
	switch mt {
	case MapTypeLPMTrie:
		featureString = fmt.Sprintf("#define HAVE_LPM_MAP_TYPE")
	case MapTypeLRUHash:
		featureString = fmt.Sprintf("#define HAVE_LRU_MAP_TYPE")
	default:
		break
	}
	return featureString
}

// ReadFeatureProbes reads the bpf_features.h file at the specified path (as
// generated by bpf/run_probes.sh), and stores the results of the kernel
// feature probing.
func ReadFeatureProbes(filename string) {
	f, err := os.Open(filename)
	if err != nil {
		// Should not happen; the caller ensured that the file exists
		log.WithFields(logrus.Fields{
			logfields.Path: filename,
		}).WithError(err).Fatal("Failed to read feature probes")
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		for mapType := MapTypeHash; mapType < MapTypeMaximum; mapType++ {
			featureString := mapTypeToFeatureString(mapType)
			if featureString != "" &&
				bytes.Compare(scanner.Bytes(), []byte(featureString)) == 0 {
				log.Debugf("Detected support for map type %s", mapType.String())
				supportedMapTypes[mapType] = true
			}
		}
	}

	for mapType := MapTypeHash; mapType < MapTypeMaximum; mapType++ {
		if mapTypeToFeatureString(mapType) == "" {
			log.Debugf("Skipping support detection for map type %s", mapType.String())
		} else if _, probed := supportedMapTypes[mapType]; !probed {
			log.Debugf("Detected no support for map type %s", mapType.String())
			supportedMapTypes[mapType] = false
		}
	}
}

// GetMapType determines whether the specified map type is supported by the
// kernel (as determined by ReadFeatureProbes()), and if the map type is not
// supported, returns a more primitive map type that may be used to implement
// the map on older implementations. Otherwise, returns the specified map type.
func GetMapType(t MapType) MapType {
	switch t {
	case MapTypeLPMTrie:
		fallthrough
	case MapTypeLRUHash:
		if !supportedMapTypes[t] {
			return MapTypeHash
		}
	}
	return t
}
