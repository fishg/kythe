/*
 * Copyright 2014 Google Inc. All rights reserved.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

// Binary triples implements a converter from an Entry stream to a stream of triples.
//
// Examples:
//   triples < entries > triples.nq
//   triples entries > triples.nq.gz
//   triples --graphstore path/to/gs > triples.nq.gz
//   triples entries triples.nq
//
// Reference: http://en.wikipedia.org/wiki/N-Triples
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"kythe/go/storage"
	"kythe/go/storage/gsutil"
	"kythe/go/storage/stream"
	"kythe/go/util/kytheuri"
	"kythe/go/util/schema"

	spb "kythe/proto/storage_proto"
)

func usage() {
	binary := filepath.Base(os.Args[0])
	fmt.Fprintf(os.Stderr, "Usage: %s [(--graphstore path | entries_file) [triples_out]]\n", binary)
	flag.PrintDefaults()
}

var (
	keepReverseEdges = flag.Bool("keep_reverse_edges", false, "Do not filter reverse edges from triples output")
	quiet            = flag.Bool("quiet", false, "Do not emit logging messages")

	gs storage.GraphStore
)

func init() {
	gsutil.Flag(&gs, "graphstore", "Path to GraphStore to convert to triples (instead of an entry stream)")
	flag.Usage = usage
}

func main() {
	flag.Parse()

	if len(flag.Args()) > 2 || (gs != nil && len(flag.Args()) > 1) {
		fmt.Fprintf(os.Stderr, "ERROR: too many arguments %v\n", flag.Args())
		flag.Usage()
		os.Exit(1)
	}

	in := os.Stdin
	if gs == nil && len(flag.Args()) > 0 {
		file, err := os.Open(flag.Arg(0))
		if err != nil {
			log.Fatalf("Failed to open input file %q: %v", flag.Arg(0), err)
		}
		defer file.Close()
		in = file
	}

	outIdx := 1
	if gs != nil {
		outIdx = 0
	}

	out := os.Stdout
	if len(flag.Args()) > outIdx {
		file, err := os.Create(flag.Arg(outIdx))
		if err != nil {
			log.Fatalf("Failed to create output file %q: %v", flag.Arg(outIdx), err)
		}
		defer file.Close()
		out = file
	}

	var (
		entries      <-chan *spb.Entry
		reverseEdges int
		triples      int
	)

	if gs == nil {
		entries = stream.ReadEntries(in)
	} else {
		ch := make(chan *spb.Entry)
		entries = ch
		go func() {
			defer close(ch)
			if err := gs.Scan(&spb.ScanRequest{}, ch); err != nil {
				log.Fatalf("GraphStore Scan error: %v", err)
			}
		}()
	}

	for entry := range entries {
		if schema.EdgeDirection(entry.GetEdgeKind()) == schema.Reverse && !*keepReverseEdges {
			reverseEdges++
			continue
		}

		t, err := toTriple(entry)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Fprintln(out, t)
		triples++
	}

	if !*quiet {
		if !*keepReverseEdges {
			log.Printf("Skipped %d reverse edges", reverseEdges)
		}
		log.Printf("Wrote %d triples", triples)
	}
}

type triple struct {
	Subject   string
	Predicate string
	Object    string
}

func (t *triple) String() string {
	return fmt.Sprintf("%q %q %q .", t.Subject, t.Predicate, t.Object)
}

// toTriple converts an Entry to the triple file format. Returns an error if the
// entry is not valid.
func toTriple(entry *spb.Entry) (*triple, error) {
	if entry.Source == nil || (entry.Target == nil) != (entry.GetEdgeKind() == "") {
		return nil, fmt.Errorf("invalid entry: %v", entry)
	}

	t := &triple{
		Subject: kytheuri.FromVName(entry.Source).String(),
	}
	if entry.GetEdgeKind() != "" {
		t.Predicate = entry.GetEdgeKind()
		t.Object = kytheuri.FromVName(entry.Target).String()
	} else {
		t.Predicate = entry.GetFactName()
		t.Object = string(entry.GetFactValue())
	}
	return t, nil
}
