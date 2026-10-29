package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"
)

var erroredUpstream = errors.New("errored upstream")

type paymentGatewayPostPaymentRequest struct {
	Amount int `json:"amount"`
}

type paymentGatewayGetPaymentsResponseOne struct {
	Amount int    `json:"amount"`
	Status string `json:"status"`
}

// requestPaymentGatewayPostPayment posts one payment, identified by idempotencyKey (the ride id), to the payment
// gateway. The gateway dedupes on `Idempotency-Key` (app.md「Idempotency-Key ヘッダを利用したリクエストの重複防止」,
// webapp/payment_mock/openapi.yaml), so a failed attempt is simply re-sent with the same key. The HTTP calls run on a
// context that does not inherit the request's cancellation: if the bench drops the evaluation request mid-flight,
// the payment must still finish consistently with the ride (a half-done payment + rolled-back tx would be charged
// again on the next evaluation = "決済サーバーに誤った支払い").
// After the retries, the reference's check (GET /payments count == rides count) decides success as before.
func requestPaymentGatewayPostPayment(ctx context.Context, paymentGatewayURL string, token string, idempotencyKey string, param *paymentGatewayPostPaymentRequest, retrieveRidesOrderByCreatedAtAsc func() ([]Ride, error)) error {
	b, err := json.Marshal(param)
	if err != nil {
		return err
	}
	ctx = context.WithoutCancel(ctx)

	var lastErr error
	for retry := 0; retry < 6; retry++ {
		if retry > 0 {
			time.Sleep(100 * time.Millisecond)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, paymentGatewayURL+"/payments", bytes.NewBuffer(b))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Idempotency-Key", idempotencyKey)

		res, err := http.DefaultClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}
		res.Body.Close()
		if res.StatusCode == http.StatusNoContent {
			return nil
		}
		lastErr = fmt.Errorf("[POST /payments] unexpected status code (%d)", res.StatusCode)
	}

	// still not confirmed: ask the gateway what it recorded (same rule as the reference implementation)
	getReq, err := http.NewRequestWithContext(ctx, http.MethodGet, paymentGatewayURL+"/payments", bytes.NewBuffer([]byte{}))
	if err != nil {
		return err
	}
	getReq.Header.Set("Authorization", "Bearer "+token)

	getRes, err := http.DefaultClient.Do(getReq)
	if err != nil {
		return err
	}
	defer getRes.Body.Close()

	// GET /payments は障害と関係なく200が返るので、200以外は回復不能なエラーとする
	if getRes.StatusCode != http.StatusOK {
		return fmt.Errorf("[GET /payments] unexpected status code (%d)", getRes.StatusCode)
	}
	var payments []paymentGatewayGetPaymentsResponseOne
	if err := json.NewDecoder(getRes.Body).Decode(&payments); err != nil {
		return err
	}

	rides, err := retrieveRidesOrderByCreatedAtAsc()
	if err != nil {
		return err
	}

	if len(rides) != len(payments) {
		return fmt.Errorf("unexpected number of payments: %d != %d (last error: %v). %w", len(rides), len(payments), lastErr, erroredUpstream)
	}

	return nil
}
