package gosmpp_test

import (
	"log"
	"os"
	"strings"
	"time"

	"github.com/linxGnu/gosmpp"
	"github.com/linxGnu/gosmpp/data"
	"github.com/linxGnu/gosmpp/errors"
	"github.com/linxGnu/gosmpp/pdu"
)

type MessageSender struct {
	smppSession *gosmpp.Session
	srcAddr     pdu.Address
}

func NewMessageSender() (*MessageSender, error) {
	auth := gosmpp.Auth{
		SMSC:       os.Getenv("SMPP_SMSC"),
		SystemID:   os.Getenv("SMPP_SYSTEM_ID"),
		Password:   os.Getenv("SMPP_PASSWORD"),
		SystemType: os.Getenv("SMPP_SYSTEM_TYPE"),
	}
	connector := gosmpp.TRXConnector(gosmpp.NonTLSDialer, auth)
	settings := gosmpp.Settings{
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		EnquireLink:  5 * time.Second,

		OnPDU: handlePDU(),

		OnReceivingError: func(err error) {
			log.Println("OnReceivingError:", err)
		},

		OnSubmitError: func(_ pdu.PDU, err error) {
			log.Println("OnSubmitError: ", err)
		},

		OnRebindingError: func(err error) {
			log.Println("OnRebindingError:", err)
		},

		OnClosed: func(state gosmpp.State) {
			log.Println("OnClosed:", state.String())
		},
	}
	smppSession, err := gosmpp.NewSession(connector, settings, 5*time.Second)
	if err != nil {
		return nil, err
	}

	srcAddr, err := pdu.NewAddressWithTonNpiAddr(
		data.GSM_TON_UNKNOWN,
		data.GSM_NPI_UNKNOWN,
		os.Getenv("SMPP_SOURCE_ADDR"),
	)
	if err != nil {
		return nil, err
	}

	return &MessageSender{smppSession: smppSession, srcAddr: srcAddr}, nil
}

func (ms *MessageSender) SendMessage(to, message string) error {
	destAddr, err := pdu.NewAddressWithTonNpiAddr(
		data.GSM_TON_UNKNOWN,
		data.GSM_NPI_UNKNOWN,
		to,
	)
	if err != nil {
		return err
	}

	submitSM := pdu.NewSubmitSM().(*pdu.SubmitSM)
	submitSM.SourceAddr = ms.srcAddr
	submitSM.DestAddr = destAddr

	var parts []*pdu.SubmitSM
	needsSplit := false

	if len(data.ValidateGSM7String(message)) > 0 {
		err = submitSM.Message.SetMessageWithEncoding(message, data.UCS2)
		if err == errors.ErrShortMessageLengthTooLarge {
			err = submitSM.Message.SetLongMessageWithEnc(message, data.UCS2)
			needsSplit = true
		}
	} else {
		err = submitSM.Message.SetMessageWithEncoding(message, data.GSM7BIT)
		if err == errors.ErrShortMessageLengthTooLarge {
			err = submitSM.Message.SetLongMessageWithEnc(message, data.GSM7BIT)
			needsSplit = true
		}
	}
	if err != nil {
		return err
	}

	// request for delivery receipts, handlePDU needs to handle DeliverSM
	// default: data.DFLT_REG_DELIVERY
	submitSM.RegisteredDelivery = data.SM_SMSC_RECEIPT_REQUESTED

	if needsSplit || submitSM.ShouldSplit() {
		parts, err = submitSM.Split()
		if err != nil {
			return err
		}
	} else {
		parts = append(parts, submitSM)
	}

	for i, part := range parts {
		log.Printf("SubmitSM %d (part %d) (%s)\n", part.SequenceNumber, i, to)
		if err = ms.smppSession.Transceiver().Submit(part); err != nil {
			log.Println(err)
			return err
		}
	}

	return nil
}

func handlePDU() func(pdu.PDU, bool) {
	concatenated := map[uint8][]string{}
	return func(p pdu.PDU, _ bool) {
		switch pd := p.(type) {
		case *pdu.SubmitSMResp:
			log.Println("SubmitSMResp:", pd.SequenceNumber, pd.MessageID, pd.IsOk())

		case *pdu.GenericNack:
			log.Println("GenericNack Received")

		case *pdu.EnquireLinkResp:
			log.Println("EnquireLinkResp Received")

		case *pdu.DataSM:
			log.Printf("DataSM:%+v\n", pd)

		case *pdu.DeliverSM:
			log.Println("DeliverSM:", pd.SequenceNumber, pd.IsOk())
			// region concatenated sms (sample code)
			message, err := pd.Message.GetMessage()
			log.Println(message, err)
			if err != nil {
				log.Fatal(err)
			}

			// found means there's UDH and the message is concatenated.
			// check the #totalParts and see if all the DeliverSM parts are received
			// if not, then keep concatenating; otherwise, concatenating is done and reset
			totalParts, sequence, reference, found := pd.Message.UDH().GetConcatInfo()
			if found {
				if _, ok := concatenated[reference]; !ok {
					concatenated[reference] = make([]string, totalParts)
				}
				concatenated[reference][sequence-1] = message
			}
			if !found {
				//log.Println(message)
			} else if parts, ok := concatenated[reference]; ok && isConcatenatedDone(parts, totalParts) {
				log.Println(strings.Join(parts, ""))
				delete(concatenated, reference)
			}
			// endregion
		}
	}
}

func isConcatenatedDone(parts []string, total byte) bool {
	for _, part := range parts {
		if part != "" {
			total--
		}
	}
	return total == 0
}

func (ms *MessageSender) Close() error {
	return ms.smppSession.Close()
}
