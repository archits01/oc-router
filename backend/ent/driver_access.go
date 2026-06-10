package ent

import "entgo.io/ent/dialect"

// Driver
func (c *Client) Driver() dialect.Driver {
	return c.driver
}
