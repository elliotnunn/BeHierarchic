package hfs

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

var errNotBtree = errors.New("not a b-tree")

func parseBTree(tree io.ReaderAt) (records []bRecord, err error) {
	// Special first headNode has special header record
	var headNode [512]byte
	n, err := tree.ReadAt(headNode[:], 0)
	if n != len(headNode) {
		return nil, fmt.Errorf("b-tree header read error: %w", err)
	}

	if headNode[8] != 1 || headNode[9] != 0 {
		return nil, errNotBtree
	}

	header, err := parseBNode(nil, &headNode)
	if err != nil {
		return nil, fmt.Errorf("b-tree header structure error: %w", err)
	} else if len(header) < 1 || len(header[0]) < 18 {
		return nil, errors.New("b-tree header structure error: missing header record")
	}

	// Ends of a linked list of leaf nodes
	bthFNode := binary.BigEndian.Uint32(header[0][10:])
	bthLNode := binary.BigEndian.Uint32(header[0][14:])

	i := uint32(bthFNode)
	seen := make(map[uint32]bool)
	for {
		if seen[i] {
			return nil, errors.New("b-tree node loop")
		}
		seen[i] = true

		node := new([512]byte)
		offset := 512 * int64(i)
		n, err := tree.ReadAt(node[:], offset)
		if n != len(node) {
			return nil, fmt.Errorf("b-tree read error at offset %#x: %w", offset, err)
		}

		records, err = parseBNode(records, node)
		if err != nil {
			return nil, fmt.Errorf("b-tree structure error at offset %#x: %w", offset, err)
		}

		if i == bthLNode {
			break
		}
		i = binary.BigEndian.Uint32(node[:])
	}
	return records, nil
}

func parseBNode(list []bRecord, node *[512]byte) ([]bRecord, error) {
	// 14 byte header
	cnt := binary.BigEndian.Uint16(node[10:])
	if cnt > 248 {
		return nil, fmt.Errorf("%d records exceeds maximum", cnt)
	}

	lowlimit, highlimit := uint16(14), uint16(512-2*(cnt+1))

	for i := range cnt {
		start := binary.BigEndian.Uint16(node[512-2-2*i:])
		end := binary.BigEndian.Uint16(node[512-4-2*i:])
		if lowlimit > start || start > end || end > highlimit {
			return nil, fmt.Errorf("record at [%#x:%#x]", start, end)
		}
		list = append(list, node[start:end])
		lowlimit = end
	}
	return list, nil
}
