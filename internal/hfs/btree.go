package hfs

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

func parseBTree(tree io.ReaderAt) (records []bRecord, err error) {
	// Special first headNode has special header record
	var headNode [512]byte
	n, err := tree.ReadAt(headNode[:], 0)
	if n != len(headNode) {
		return nil, fmt.Errorf("b-tree read error: %w", err)
	}
	header, err := parseBNode(nil, &headNode)
	if err != nil {
		return nil, err
	} else if len(header) < 1 || len(header[0]) < 18 {
		return nil, errors.New("b-tree structure error: bad header node")
	}

	// Ends of a linked list of leaf nodes
	bthFNode := binary.BigEndian.Uint32(header[0][10:])
	bthLNode := binary.BigEndian.Uint32(header[0][14:])

	i := uint32(bthFNode)
	seen := make(map[uint32]bool)
	for {
		if seen[i] {
			return nil, errors.New("b-tree structure error: node loop")
		}
		seen[i] = true

		node := new([512]byte)
		n, err := tree.ReadAt(node[:], 512*int64(i))
		if n != len(node) {
			return nil, fmt.Errorf("b-tree read error: %w", err)
		}

		records, err = parseBNode(records, node)
		if err != nil {
			return nil, err
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
		return nil, fmt.Errorf("b-tree structure error: %d records exceeds maximum", cnt)
	}

	lowlimit, highlimit := uint16(14), uint16(512-2*(cnt+1))

	for i := range cnt {
		start := binary.BigEndian.Uint16(node[512-2-2*i:])
		end := binary.BigEndian.Uint16(node[512-4-2*i:])
		if lowlimit > start || start > end || end > highlimit {
			return nil, fmt.Errorf("b-tree structure error: record at [%d:%d]", start, end)
		}
		list = append(list, node[start:end])
		lowlimit = end
	}
	return list, nil
}
