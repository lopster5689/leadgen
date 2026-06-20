package main

import "context"

// Source is one directory the pipeline can crawl. Adding breadth = adding a
// case to buildSource with a Discover (list -> member refs) and Fetch
// (ref -> parsed Lead) pair, the same shape as the Delhi Chamber parser.
type Source struct {
	Name     string
	Discover func(ctx context.Context, c *Client) ([]memberRef, error)
	Fetch    func(ctx context.Context, c *Client, ref memberRef) (*Lead, error)
}

// sourceNames lists registered sources (for `leadgen sources`).
var sourceNames = []string{"delhichamber", "website"}

func buildSource(name, letters string) (Source, bool) {
	switch name {
	case "delhichamber":
		return Source{
			Name: "delhichamber",
			Discover: func(ctx context.Context, c *Client) ([]memberRef, error) {
				return discoverMembers(ctx, c, letters)
			},
			Fetch: fetchLead,
		}, true
	case "website":
		return Source{Name: "website", Discover: discoverWebsite, Fetch: fetchWebsite}, true
	}
	return Source{}, false
}
