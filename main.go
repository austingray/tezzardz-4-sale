package main

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/aws/aws-sdk-go/service/s3/s3manager"
	"github.com/dghubble/go-twitter/twitter"
	"github.com/dghubble/oauth1"
	"github.com/hasura/go-graphql-client"
)

var region = os.Getenv("AWS_REGION")
var bucketName = os.Getenv("AWS_S3_BUCKET")
var fileName = "index"

var consumerKey = os.Getenv("TWTR_CONSUMER_KEY")
var consumerSecret = os.Getenv("TWTR_CONSUMER_SECRET")
var accessToken = os.Getenv("TWTR_ACCESS_TOKEN")
var accessSecret = os.Getenv("TWTR_ACCESS_SECRET")

type Transaction struct {
	Type      string
	Objkt_id  graphql.Int
	Price     int64
	Timestamp graphql.String
	Buyer_id  graphql.String
	Seller_id graphql.String
}

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

type Dutch_auction struct {
	Status           graphql.String
	Buyer_id         graphql.String
	Buy_price        int64
	Update_timestamp graphql.String
	Objkt_id         graphql.Int
	Creator_id       graphql.String
}

type English_auction struct {
	Creator_id       graphql.String
	Objkt_id         graphql.Int
	Update_timestamp graphql.String
	Status           graphql.String
	Reserve          int64
	Bids             []struct {
		Bidder_id graphql.String
		Amount    int64
	}
}

type Query struct {
	Hic_et_nunc_fa2 []struct {
		Asks             []Ask
		Bids             []Bid
		Dutch_auctions   []Dutch_auction
		English_auctions []English_auction
	} `graphql:"hic_et_nunc_fa2(where: {contract: {_eq: \"KT1LHHLso8zQWQWg1HUukajdxxbkGfNoHjh6\"}})"`
}

func main() {
	lambda.Start(HandleLambdaEvent)
}

func HandleLambdaEvent() {
	var query Query

	client := graphql.NewClient("https://api.hicdex.com/v1/graphql/", nil)

	err := client.Query(context.Background(), &query, nil)
	if err != nil {
		fmt.Println(err.Error())
	}

	var transactions []Transaction

	// handle asks
	var asks []Ask
	for _, ask := range query.Hic_et_nunc_fa2[0].Asks {
		if ask.Status == "concluded" {
			asks = append(asks, ask)
		}
	}
	for _, k := range asks {
		if k.Status == "concluded" {
			// log.Printf("%v\n", k)

			trx := Transaction{
				Type:      "asks",
				Objkt_id:  k.Objkt_id,
				Buyer_id:  k.Fulfilled[0].Buyer_id,
				Seller_id: k.Fulfilled[0].Seller_id,
				Price:     k.Price,
				Timestamp: k.Fulfilled[0].Timestamp,
			}

			transactions = append(transactions, trx)
		}
	}

	// handle bids
	for _, k := range query.Hic_et_nunc_fa2[0].Bids {
		if k.Status == "concluded" {
			// log.Printf("%v\n", k)

			trx := Transaction{
				Type:      "bids",
				Objkt_id:  k.Objkt_id,
				Buyer_id:  k.Creator_id,
				Seller_id: k.Seller_id,
				Price:     k.Price,
				Timestamp: k.Timestamp,
			}

			transactions = append(transactions, trx)
		}
	}

	// handle dutch auctions
	for _, k := range query.Hic_et_nunc_fa2[0].Dutch_auctions {
		if k.Status == "concluded" {
			// log.Printf("%v\n", k)

			trx := Transaction{
				Type:      "dutch_auctions",
				Objkt_id:  k.Objkt_id,
				Buyer_id:  k.Buyer_id,
				Seller_id: k.Creator_id,
				Price:     k.Buy_price,
				Timestamp: k.Update_timestamp,
			}

			transactions = append(transactions, trx)
		}
	}

	// handle english auctions
	for _, k := range query.Hic_et_nunc_fa2[0].English_auctions {
		if k.Status == "concluded" {
			display := false
			for _, l := range k.Bids {
				if l.Amount > k.Reserve {
					display = true
				}
			}

			if display {
				// log.Printf("%v\n", k)

				trx := Transaction{
					Type:      "english_auctions",
					Objkt_id:  k.Objkt_id,
					Buyer_id:  k.Bids[len(k.Bids)-1].Bidder_id,
					Seller_id: k.Creator_id,
					Price:     k.Bids[len(k.Bids)-1].Amount,
					Timestamp: k.Update_timestamp,
				}

				transactions = append(transactions, trx)
			}
		}
	}

	// sort
	sort.Slice(transactions, func(i, j int) bool {
		return transactions[i].Timestamp < transactions[j].Timestamp
	})
	// for _, t := range transactions {
	// 	log.Printf("%v\n", t)
	// }

	// old index
	old := downloadFromS3()
	new := len(transactions)
	if new > old {
		log.Println("yes new")
		log.Println(strconv.Itoa(new) + " | " + strconv.Itoa(old))
		uploadToS3([]byte(strconv.Itoa(new)))
		// fire off the tweet

		// get the new
		trxs := transactions[old-1:]
		for _, trx := range trxs {
			log.Printf("%v\n", trx)
			tweetTrx(trx)
		}
	} else {
		// log.Println("no new")
		// log.Println(strconv.Itoa(new) + " | " + strconv.Itoa(old))
	}

	return
}

func uploadToS3(byteData []byte) {
	// The session the S3 Uploader will use
	sess := session.Must(session.NewSession(&aws.Config{
		Region: aws.String(region),
	}))

	// Create an uploader with the session and default options
	uploader := s3manager.NewUploader(sess)

	r := bytes.NewReader(byteData)

	// Upload the file to S3.
	result, err := uploader.Upload(&s3manager.UploadInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(fileName),
		Body:   r,
	})
	if err != nil {
		log.Printf("failed to upload file, %v", err)
	}

	log.Printf("file uploaded to, %s\n", result.Location)
}

func downloadFromS3() int {
	// The session the S3 Downloader will use
	sess := session.Must(session.NewSession(&aws.Config{
		Region: aws.String(region)}))

	// Create a downloader with the session and default options
	downloader := s3manager.NewDownloader(sess)

	// Write the contents of S3 Object to the file
	buf := aws.NewWriteAtBuffer([]byte{})
	_, err := downloader.Download(buf, &s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(fileName),
	})
	if err != nil {
		log.Printf("failed to download file, %v", err)
	}

	ival, _ := strconv.Atoi(string(buf.Bytes()))
	return ival
}

func tweetTrx(trx Transaction) {
	// consumer key, consumer secret
	// access token, access secret
	config := oauth1.NewConfig(consumerKey, consumerSecret)
	token := oauth1.NewToken(accessToken, accessSecret)
	httpClient := config.Client(oauth1.NoContext, token)
	client := twitter.NewClient(httpClient)

	// cid := data[strconv.Itoa(int(trx.Objkt_id))]
	// url := "https://cloudflare-ipfs.com/ipfs/" + cid

	// base64media := remoteToBase64(url)
	// mediaId := uploadMedia(base64media)
	// mediaIds := make([]int64, 0)
	// mediaIds = append(mediaIds, mediaId)

	body := "This snazzy little fukr just sold for " + strconv.Itoa(int(trx.Price)/1000000) + " tez.\n\n"
	body = body + "https://objkt.com/asset/tezzardz/" + strconv.Itoa(int(trx.Objkt_id))

	_, _, err := client.Statuses.Update(body, nil)
	if err != nil {
		log.Println(err)
	}
}

// func uploadMedia(base64 string) int64 {
// 	api := anaconda.NewTwitterApiWithCredentials(accessToken, accessSecret, consumerKey, consumerSecret)
// 	media, err := api.UploadMedia(base64)
// 	if err != nil {
// 		fmt.Println(err)
// 	}
// 	return media.MediaID
// }

// func remoteToBase64(url string) io.Writer {
// 	resp, err := http.Get(url)
// 	if err != nil {
// 		log.Fatal(err)
// 	}

// 	defer resp.Body.Close()

// 	b, err := ioutil.ReadAll(resp.Body)
// 	if err != nil {
// 		log.Fatal(err)
// 	}

// 	// Decode the image (from PNG to image.Image):
// 	src, _ := png.Decode(bytes.NewReader(b))

// 	// Set the expected size that you want:
// 	dst := image.NewRGBA(image.Rect(0, 0, src.Bounds().Max.X/5, src.Bounds().Max.Y/5))

// 	// Resize:
// 	draw.NearestNeighbor.Scale(dst, dst.Rect, src, src.Bounds(), draw.Over, nil)

// 	buf := new(bytes.Buffer)
// 	if err := jpeg.Encode(buf, dst, nil); err != nil {
// 		log.Println(errors.Wrap(err, "unable to encode jpeg"))
// 	}

// 	return buf

// 	// var base64Encoding string
// 	// base64Encoding += "data:image/jpeg;base64,"
// 	// base64Encoding += base64.StdEncoding.EncodeToString(buf.Bytes())
// 	// log.Println(base64Encoding)
// 	// return base64Encoding
// }
