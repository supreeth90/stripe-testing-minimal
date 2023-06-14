### Supported Rest calls ###
1. Create Customer and Subscription.
    POST http://localhost:4242/createcustomersubscription
    Body: {"email":"test@example.com"}
    Response: {"subscriptionId":"<id>","clientSecret":"<client-secret>"}
2. Create Customer Portal to manage subscriptions.
    POST http://localhost:4242/createportal
    Body: {"email":"test@example.com"}
    redirects to customer portal