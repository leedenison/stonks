// Package join gathers the rows of several tables into one value anchored on
// a row of one of them. A join is named for its anchor. A join that adds a
// table to another join embeds it. A function that fills a join takes a
// batch of anchors and reads each further table once for the whole batch.
package join
