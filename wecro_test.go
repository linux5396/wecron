package main

import (
	"fmt"
	"testing"
)

/**
* @author anton lin
* @date 2020/5/27
 */
const (
	a int =1<<iota
	b
	c
)
func TestA2(t *testing.T) {
	fmt.Print(a,b,c)
}