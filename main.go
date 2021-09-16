package main

import (
	"context"
	"fmt"
	"log"
	"sort"

	"github.com/hasura/go-graphql-client"
)

type Ask struct {
	Price     int64
	Fulfilled []struct {
		Timestamp graphql.String
		Buyer_id  graphql.String
		Seller_id graphql.String
	}
	Objkt_id graphql.Int
	Status   graphql.String
}

type Bid struct {
	Price      int64
	Timestamp  graphql.String
	Creator_id graphql.String
	Seller_id  graphql.String
	Objkt_id   graphql.Int
	Status     graphql.String
}

type Query struct {
	Hic_et_nunc_fa2 []struct {
		Asks []Ask
		Bids []Bid
	} `graphql:"hic_et_nunc_fa2(where: {contract: {_eq: \"KT1LHHLso8zQWQWg1HUukajdxxbkGfNoHjh6\"}})"`
}

func main() {
	var query Query

	client := graphql.NewClient("https://api.hicdex.com/v1/graphql/", nil)

	err := client.Query(context.Background(), &query, nil)
	if err != nil {
		fmt.Println(err.Error())
	}

	var asks []Ask
	for _, ask := range query.Hic_et_nunc_fa2[0].Asks {
		if ask.Status == "concluded" {
			asks = append(asks, ask)
		}
	}
	sort.Slice(asks, func(i, j int) bool {
		return asks[i].Fulfilled[0].Timestamp < asks[j].Fulfilled[0].Timestamp
	})
	for _, k := range asks {
		if k.Status == "concluded" {
			log.Printf("%v\n", k)
		}
	}

	// for _, k := range query.Hic_et_nunc_fa2[0].Bids {
	// 	if k.Status == "concluded" {
	// 		log.Printf("%v\n", k)
	// 	}
	// }
}
