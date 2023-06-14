package main

import (
	"encoding/json"
	"fmt"
	"github.com/stripe/stripe-go/v74"
	portalsession "github.com/stripe/stripe-go/v74/billingportal/session"
	"github.com/stripe/stripe-go/v74/customer"
	"github.com/stripe/stripe-go/v74/subscription"
	"github.com/stripe/stripe-go/v74/webhook"
	"io/ioutil"
	"log"
	"net/http"
	"os"
)

func main() {
	// This is your test secret API key.
	stripe.Key = ""

	http.Handle("/", http.FileServer(http.Dir("./static")))
	http.HandleFunc("/webhook", handleWebhook)

	http.HandleFunc("/createcustomersubscription", handleCreateSubscriptionAndCustomer)
	http.HandleFunc("/createportal", handleCreatePortalSession)

	addr := "localhost:4242"
	log.Printf("Listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}

func handleWebhook(w http.ResponseWriter, req *http.Request) {
	const MaxBodyBytes = int64(65536)
	req.Body = http.MaxBytesReader(w, req.Body, MaxBodyBytes)
	payload, err := ioutil.ReadAll(req.Body)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading request body: %v\n", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}

	event := stripe.Event{}

	if err := json.Unmarshal(payload, &event); err != nil {
		fmt.Fprintf(os.Stderr, "⚠️  Webhook error while parsing basic request. %v\n", err.Error())
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Replace this endpoint secret with your endpoint's unique secret
	// If you are testing with the CLI, find the secret by running 'stripe listen'
	// If you are using an endpoint defined with the API or dashboard, look in your webhook settings
	// at https://dashboard.stripe.com/webhooks
	endpointSecret := ""
	signatureHeader := req.Header.Get("Stripe-Signature")
	event, err = webhook.ConstructEvent(payload, signatureHeader, endpointSecret)
	if err != nil {
		fmt.Fprintf(os.Stderr, "⚠️  Webhook signature verification failed. %v\n", err)
		w.WriteHeader(http.StatusBadRequest) // Return a 400 error on a bad signature
		return
	}
	// Unmarshal the event data into an appropriate struct depending on its Type
	switch event.Type {
	case "payment_intent.succeeded":
		var paymentIntent stripe.PaymentIntent
		err := json.Unmarshal(event.Data.Raw, &paymentIntent)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing webhook JSON: %v\n", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		log.Printf("Successful payment for %d.", paymentIntent.Amount)
		// Then define and call a func to handle the successful payment intent.
		// handlePaymentIntentSucceeded(paymentIntent)
	case "payment_method.attached":
		var paymentMethod stripe.PaymentMethod
		err := json.Unmarshal(event.Data.Raw, &paymentMethod)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing webhook JSON: %v\n", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		// Then define and call a func to handle the successful attachment of a PaymentMethod.
		// handlePaymentMethodAttached(paymentMethod)
	case "customer.created":
		var customer stripe.Customer
		err := json.Unmarshal(event.Data.Raw, &customer)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error parsing webhook JSON: %v\n", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		log.Printf("Customer created. name: %s, id: %s, email: %s", customer.Name, customer.ID, customer.Email)
	default:
		fmt.Fprintf(os.Stderr, "Unhandled event type: %s\n", event.Type)
	}

	w.WriteHeader(http.StatusOK)
}

func handleCreateSubscriptionAndCustomer(w http.ResponseWriter, req *http.Request) {

	if req.Method != "POST" {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	var customerBody struct {
		Email string `json:"email"`
	}

	if err := json.NewDecoder(req.Body).Decode(&customerBody); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		log.Printf("json.NewDecoder.Decode: %v", err)
		return
	}

	//body, err := io.ReadAll(req.Body)
	//if err != nil {
	//	w.WriteHeader(http.StatusServiceUnavailable)
	//	//writeJSON(w, nil, err)
	//	log.Printf("failed to read body: %v", err)
	//	return
	//}
	//
	//log.Printf("body: %s", string(body))
	//
	//err = json.Unmarshal(body, customerBody)
	//if err != nil {
	//	w.WriteHeader(http.StatusServiceUnavailable)
	//	//writeJSON(w, nil, err)
	//	log.Printf("failed to unmarshall body: %v", err)
	//	return
	//}

	log.Printf("email : %s", customerBody.Email)

	// find if a customer exists by the email ID
	i := customer.List(&stripe.CustomerListParams{Email: stripe.String(customerBody.Email)})

	customerObj := &stripe.Customer{}
	for i.Next() {
		c := i.Customer()
		customerObj = c
		log.Printf("customer found with email: %s", customerBody.Email)
		break
	}
	err := fmt.Errorf("")
	if customerObj.Email == "" {
		log.Printf("customer not found with email: %s, trying to create one", customerBody.Email)

		params := &stripe.CustomerParams{
			Email: stripe.String(customerBody.Email),
			Name:  stripe.String(customerBody.Email),
		}
		customerObj, err = customer.New(params)
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			//writeJSON(w, nil, err)
			log.Printf("failed to create customer object in stripe: %v", err)
			return
		}

	}

	// Find a subscription for this customer
	subsListParams := stripe.SubscriptionListParams{Customer: stripe.String(customerObj.ID)}
	subsListParams.AddExpand("data.pending_setup_intent")
	subsIter := subscription.List(&subsListParams)

	subscriptionObj := &stripe.Subscription{}
	subs := subsIter.SubscriptionList().Data
	if len(subs) == 0 {
		log.Printf("no subscriptions found for customer: %s", customerObj.ID)

		// Automatically save the payment method to the subscription
		// when the first payment is successful.
		paymentSettings := &stripe.SubscriptionPaymentSettingsParams{
			SaveDefaultPaymentMethod: stripe.String("on_subscription"),
		}

		// Create the subscription. Note we're expanding the Subscription's
		// latest invoice and that invoice's payment_intent
		// so we can pass it to the front end to confirm the payment
		subParams := &stripe.SubscriptionParams{
			Customer: stripe.String(customerObj.ID),
			Items: []*stripe.SubscriptionItemsParams{
				{
					// adds metal.large
					Price: stripe.String("price_1NDv4ZE0InFu2MN0gMRZqlGe"),
				},
			},
			PaymentSettings: paymentSettings,
			PaymentBehavior: stripe.String("default_incomplete"),
		}
		subParams.AddExpand("pending_setup_intent")

		subscriptionObj, err = subscription.New(subParams)
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			//writeJSON(w, nil, err)
			log.Printf("failed to create subscription object in stripe: %v", err)
			return
		}

	} else {
		log.Printf("subscriptions found for customer: %s", customerObj.ID)
		subscriptionObj = subs[0]
	}

	log.Printf("subscriptions ID is %s", subscriptionObj.ID)
	log.Printf("ClientSecret is %s", subscriptionObj.PendingSetupIntent.ClientSecret)

	var subscriptionOut struct {
		SubscriptionID string `json:"subscriptionId"`
		ClientSecret   string `json:"clientSecret"`
	}
	subscriptionOut.SubscriptionID = subscriptionObj.ID
	subscriptionOut.ClientSecret = subscriptionObj.PendingSetupIntent.ClientSecret
	//log.Printf("ClientSecret is %s", subscriptionOut.ClientSecret)
	respBody, err := json.Marshal(subscriptionOut)
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		//writeJSON(w, nil, err)
		log.Printf("failed to marshal subscription body: %v", err)
		return
	}
	_, err = w.Write(respBody)
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		//writeJSON(w, nil, err)
		log.Printf("failed to write output: %v", err)
		return
	}
}

func handleCreatePortalSession(w http.ResponseWriter, req *http.Request) {

	if req.Method != "POST" {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	var customerBody struct {
		Email string `json:"email"`
	}

	if err := json.NewDecoder(req.Body).Decode(&customerBody); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		log.Printf("json.NewDecoder.Decode: %v", err)
		return
	}

	// Find a customer with this email ID
	// find if a customer exists by the email ID
	i := customer.List(&stripe.CustomerListParams{Email: stripe.String(customerBody.Email)})
	customers := i.CustomerList().Data
	if len(customers) == 0 {
		w.WriteHeader(http.StatusServiceUnavailable)
		log.Printf("No customer found with email ID: %s", customerBody.Email)
		return
	}

	customerObj := customers[0]

	// The URL to which the user is redirected when they are done managing
	// billing in the portal.
	returnURL := "http://localhost:4242/index.html"
	customerID := customerObj.ID

	params := &stripe.BillingPortalSessionParams{
		Customer:  stripe.String(customerID),
		ReturnURL: stripe.String(returnURL),
	}
	ps, err := portalsession.New(params)
	if err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		//writeJSON(w, nil, err)
		log.Printf("portal creation failed: %v", err)
		return
	}

	log.Printf("Created sessom URL %s", ps.URL)

	http.Redirect(w, req, ps.URL, http.StatusSeeOther)

}
