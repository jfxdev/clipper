// Package dynamostore implements store.Store backed by DynamoDB. Expiry
// uses the native TTL attribute ("expiresAt", Unix epoch seconds); the AWS
// TTL sweep is best-effort (up to ~48h delay), so Get always defensively
// re-checks expiry too. Burn-after-read uses a conditional DeleteItem so
// the get-and-delete is atomic per item.
package dynamostore

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/jfxdev/clipper/backend/internal/paste"
	"github.com/jfxdev/clipper/backend/internal/store"
)

type Config struct {
	Table string
	// Endpoint overrides the DynamoDB endpoint, e.g. for dynamodb-local in
	// dev/CI. Leave empty to use the real AWS endpoint for Region.
	Endpoint string
	Region   string
}

type Store struct {
	client *dynamodb.Client
	table  string
}

// New connects to DynamoDB and ensures the table (and its TTL
// configuration) exists. Table/TTL setup is idempotent, which matters for
// dynamodb-local in dev/CI; in a real AWS deployment the table is normally
// provisioned by infra-as-code and this becomes a no-op verification.
func New(ctx context.Context, cfg Config) (*Store, error) {
	optFns := []func(*awsconfig.LoadOptions) error{
		awsconfig.WithRegion(cfg.Region),
	}
	if cfg.Endpoint != "" {
		// dynamodb-local doesn't validate credentials but the SDK still
		// requires *some* credential provider to be configured.
		optFns = append(optFns, awsconfig.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider("local", "local", ""),
		))
	}
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx, optFns...)
	if err != nil {
		return nil, fmt.Errorf("dynamostore: load aws config: %w", err)
	}

	client := dynamodb.NewFromConfig(awsCfg, func(o *dynamodb.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
	})

	s := &Store{client: client, table: cfg.Table}
	if err := s.ensureTable(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) ensureTable(ctx context.Context) error {
	_, err := s.client.DescribeTable(ctx, &dynamodb.DescribeTableInput{TableName: aws.String(s.table)})
	var notFound *types.ResourceNotFoundException
	switch {
	case err == nil:
		// table already exists
	case errors.As(err, &notFound):
		if _, err := s.client.CreateTable(ctx, &dynamodb.CreateTableInput{
			TableName:   aws.String(s.table),
			BillingMode: types.BillingModePayPerRequest,
			KeySchema: []types.KeySchemaElement{
				{AttributeName: aws.String("id"), KeyType: types.KeyTypeHash},
			},
			AttributeDefinitions: []types.AttributeDefinition{
				{AttributeName: aws.String("id"), AttributeType: types.ScalarAttributeTypeS},
			},
		}); err != nil {
			return fmt.Errorf("dynamostore: create table: %w", err)
		}
		waiter := dynamodb.NewTableExistsWaiter(s.client)
		if err := waiter.Wait(ctx, &dynamodb.DescribeTableInput{TableName: aws.String(s.table)}, 30*time.Second); err != nil {
			return fmt.Errorf("dynamostore: wait for table active: %w", err)
		}
	default:
		return fmt.Errorf("dynamostore: describe table: %w", err)
	}

	ttl, err := s.client.DescribeTimeToLive(ctx, &dynamodb.DescribeTimeToLiveInput{TableName: aws.String(s.table)})
	if err != nil {
		return fmt.Errorf("dynamostore: describe ttl: %w", err)
	}
	if ttl.TimeToLiveDescription != nil && ttl.TimeToLiveDescription.TimeToLiveStatus == types.TimeToLiveStatusEnabled {
		return nil
	}
	if _, err := s.client.UpdateTimeToLive(ctx, &dynamodb.UpdateTimeToLiveInput{
		TableName: aws.String(s.table),
		TimeToLiveSpecification: &types.TimeToLiveSpecification{
			AttributeName: aws.String("expiresAt"),
			Enabled:       aws.Bool(true),
		},
	}); err != nil {
		return fmt.Errorf("dynamostore: enable ttl: %w", err)
	}
	return nil
}

type item struct {
	ID                string `dynamodbav:"id"`
	Data              string `dynamodbav:"data"`
	ExpiresAt         *int64 `dynamodbav:"expiresAt,omitempty"`
	BurnAfterRead     bool   `dynamodbav:"burnAfterRead"`
	PasswordProtected bool   `dynamodbav:"passwordProtected"`
	CreatedAt         string `dynamodbav:"createdAt"`
	SizeBytes         int    `dynamodbav:"sizeBytes"`
}

func toItem(p paste.Paste) item {
	it := item{
		ID:                p.ID,
		Data:              p.Data,
		BurnAfterRead:     p.BurnAfterRead,
		PasswordProtected: p.PasswordProtected,
		CreatedAt:         p.CreatedAt.UTC().Format(time.RFC3339Nano),
		SizeBytes:         p.SizeBytes,
	}
	if !p.ExpiresAt.IsZero() {
		epoch := p.ExpiresAt.Unix()
		it.ExpiresAt = &epoch
	}
	return it
}

func (it item) toPaste() (paste.Paste, error) {
	p := paste.Paste{
		ID:                it.ID,
		Data:              it.Data,
		BurnAfterRead:     it.BurnAfterRead,
		PasswordProtected: it.PasswordProtected,
		SizeBytes:         it.SizeBytes,
	}
	if it.CreatedAt != "" {
		t, err := time.Parse(time.RFC3339Nano, it.CreatedAt)
		if err != nil {
			return paste.Paste{}, err
		}
		p.CreatedAt = t
	}
	if it.ExpiresAt != nil {
		p.ExpiresAt = time.Unix(*it.ExpiresAt, 0).UTC()
	}
	return p, nil
}

func (s *Store) Create(ctx context.Context, p paste.Paste) error {
	av, err := attributevalue.MarshalMap(toItem(p))
	if err != nil {
		return err
	}
	_, err = s.client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName: aws.String(s.table),
		Item:      av,
	})
	return err
}

func (s *Store) Get(ctx context.Context, id string) (paste.Paste, error) {
	key := map[string]types.AttributeValue{"id": &types.AttributeValueMemberS{Value: id}}

	// Atomic burn: only deletes an item that is both present and marked
	// burn-after-read, so concurrent readers can never both succeed.
	del, err := s.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName:           aws.String(s.table),
		Key:                 key,
		ConditionExpression: aws.String("burnAfterRead = :true"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":true": &types.AttributeValueMemberBOOL{Value: true},
		},
		ReturnValues: types.ReturnValueAllOld,
	})

	var attrs map[string]types.AttributeValue
	var condFailed *types.ConditionalCheckFailedException
	switch {
	case err == nil:
		attrs = del.Attributes
	case errors.As(err, &condFailed):
		// Either the paste doesn't exist, or it exists but isn't
		// burn-after-read — a plain read distinguishes the two without
		// deleting anything.
		get, err := s.client.GetItem(ctx, &dynamodb.GetItemInput{TableName: aws.String(s.table), Key: key})
		if err != nil {
			return paste.Paste{}, err
		}
		if get.Item == nil {
			return paste.Paste{}, store.ErrNotFound
		}
		attrs = get.Item
	default:
		return paste.Paste{}, err
	}

	var it item
	if err := attributevalue.UnmarshalMap(attrs, &it); err != nil {
		return paste.Paste{}, err
	}
	p, err := it.toPaste()
	if err != nil {
		return paste.Paste{}, err
	}

	if p.IsExpired(time.Now()) {
		// Defensive re-check: DynamoDB TTL deletion is best-effort and can
		// lag up to ~48h, so this keeps behavior identical to the other
		// Store backends regardless of when AWS physically reaps it.
		_, _ = s.client.DeleteItem(ctx, &dynamodb.DeleteItemInput{TableName: aws.String(s.table), Key: key})
		return paste.Paste{}, store.ErrNotFound
	}
	return p, nil
}

func (s *Store) Close(ctx context.Context) error {
	return nil
}
