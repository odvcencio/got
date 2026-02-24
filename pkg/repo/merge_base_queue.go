package repo

import "github.com/odvcencio/got/pkg/object"

type mergeBaseQueueItem struct {
	hash       object.Hash
	generation uint64
}

type mergeBaseMaxHeap []mergeBaseQueueItem

func (h mergeBaseMaxHeap) Len() int { return len(h) }

func (h mergeBaseMaxHeap) Less(i, j int) bool {
	if h[i].generation == h[j].generation {
		return h[i].hash < h[j].hash
	}
	return h[i].generation > h[j].generation
}

func (h mergeBaseMaxHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

func (h *mergeBaseMaxHeap) Push(x any) {
	*h = append(*h, x.(mergeBaseQueueItem))
}

func (h *mergeBaseMaxHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	*h = old[:n-1]
	return item
}

func (h mergeBaseMaxHeap) Peek() (mergeBaseQueueItem, bool) {
	if len(h) == 0 {
		return mergeBaseQueueItem{}, false
	}
	return h[0], true
}
