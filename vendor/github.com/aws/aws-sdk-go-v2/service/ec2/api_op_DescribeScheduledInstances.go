// Code generated by smithy-go-codegen DO NOT EDIT.

package ec2

import (
	"context"
	"fmt"
	awsmiddleware "github.com/aws/aws-sdk-go-v2/aws/middleware"
	"github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/smithy-go/middleware"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

// Describes the specified Scheduled Instances or all your Scheduled Instances.
func (c *Client) DescribeScheduledInstances(ctx context.Context, params *DescribeScheduledInstancesInput, optFns ...func(*Options)) (*DescribeScheduledInstancesOutput, error) {
	if params == nil {
		params = &DescribeScheduledInstancesInput{}
	}

	result, metadata, err := c.invokeOperation(ctx, "DescribeScheduledInstances", params, optFns, c.addOperationDescribeScheduledInstancesMiddlewares)
	if err != nil {
		return nil, err
	}

	out := result.(*DescribeScheduledInstancesOutput)
	out.ResultMetadata = metadata
	return out, nil
}

// Contains the parameters for DescribeScheduledInstances.
type DescribeScheduledInstancesInput struct {

	// Checks whether you have the required permissions for the action, without
	// actually making the request, and provides an error response. If you have the
	// required permissions, the error response is DryRunOperation. Otherwise, it is
	// UnauthorizedOperation.
	DryRun *bool

	// The filters.
	//
	// * availability-zone - The Availability Zone (for example,
	// us-west-2a).
	//
	// * instance-type - The instance type (for example, c4.large).
	//
	// *
	// network-platform - The network platform (EC2-Classic or EC2-VPC).
	//
	// * platform -
	// The platform (Linux/UNIX or Windows).
	Filters []types.Filter

	// The maximum number of results to return in a single call. This value can be
	// between 5 and 300. The default value is 100. To retrieve the remaining results,
	// make another call with the returned NextToken value.
	MaxResults *int32

	// The token for the next set of results.
	NextToken *string

	// The Scheduled Instance IDs.
	ScheduledInstanceIds []string

	// The time period for the first schedule to start.
	SlotStartTimeRange *types.SlotStartTimeRangeRequest
}

// Contains the output of DescribeScheduledInstances.
type DescribeScheduledInstancesOutput struct {

	// The token required to retrieve the next set of results. This value is null when
	// there are no more results to return.
	NextToken *string

	// Information about the Scheduled Instances.
	ScheduledInstanceSet []types.ScheduledInstance

	// Metadata pertaining to the operation's result.
	ResultMetadata middleware.Metadata
}

func (c *Client) addOperationDescribeScheduledInstancesMiddlewares(stack *middleware.Stack, options Options) (err error) {
	err = stack.Serialize.Add(&awsEc2query_serializeOpDescribeScheduledInstances{}, middleware.After)
	if err != nil {
		return err
	}
	err = stack.Deserialize.Add(&awsEc2query_deserializeOpDescribeScheduledInstances{}, middleware.After)
	if err != nil {
		return err
	}
	if err = addSetLoggerMiddleware(stack, options); err != nil {
		return err
	}
	if err = awsmiddleware.AddClientRequestIDMiddleware(stack); err != nil {
		return err
	}
	if err = smithyhttp.AddComputeContentLengthMiddleware(stack); err != nil {
		return err
	}
	if err = addResolveEndpointMiddleware(stack, options); err != nil {
		return err
	}
	if err = v4.AddComputePayloadSHA256Middleware(stack); err != nil {
		return err
	}
	if err = addRetryMiddlewares(stack, options); err != nil {
		return err
	}
	if err = addHTTPSignerV4Middleware(stack, options); err != nil {
		return err
	}
	if err = awsmiddleware.AddRawResponseToMetadata(stack); err != nil {
		return err
	}
	if err = awsmiddleware.AddRecordResponseTiming(stack); err != nil {
		return err
	}
	if err = addClientUserAgent(stack); err != nil {
		return err
	}
	if err = smithyhttp.AddErrorCloseResponseBodyMiddleware(stack); err != nil {
		return err
	}
	if err = smithyhttp.AddCloseResponseBodyMiddleware(stack); err != nil {
		return err
	}
	if err = stack.Initialize.Add(newServiceMetadataMiddleware_opDescribeScheduledInstances(options.Region), middleware.Before); err != nil {
		return err
	}
	if err = addRequestIDRetrieverMiddleware(stack); err != nil {
		return err
	}
	if err = addResponseErrorMiddleware(stack); err != nil {
		return err
	}
	if err = addRequestResponseLogging(stack, options); err != nil {
		return err
	}
	return nil
}

// DescribeScheduledInstancesAPIClient is a client that implements the
// DescribeScheduledInstances operation.
type DescribeScheduledInstancesAPIClient interface {
	DescribeScheduledInstances(context.Context, *DescribeScheduledInstancesInput, ...func(*Options)) (*DescribeScheduledInstancesOutput, error)
}

var _ DescribeScheduledInstancesAPIClient = (*Client)(nil)

// DescribeScheduledInstancesPaginatorOptions is the paginator options for
// DescribeScheduledInstances
type DescribeScheduledInstancesPaginatorOptions struct {
	// The maximum number of results to return in a single call. This value can be
	// between 5 and 300. The default value is 100. To retrieve the remaining results,
	// make another call with the returned NextToken value.
	Limit int32

	// Set to true if pagination should stop if the service returns a pagination token
	// that matches the most recent token provided to the service.
	StopOnDuplicateToken bool
}

// DescribeScheduledInstancesPaginator is a paginator for
// DescribeScheduledInstances
type DescribeScheduledInstancesPaginator struct {
	options   DescribeScheduledInstancesPaginatorOptions
	client    DescribeScheduledInstancesAPIClient
	params    *DescribeScheduledInstancesInput
	nextToken *string
	firstPage bool
}

// NewDescribeScheduledInstancesPaginator returns a new
// DescribeScheduledInstancesPaginator
func NewDescribeScheduledInstancesPaginator(client DescribeScheduledInstancesAPIClient, params *DescribeScheduledInstancesInput, optFns ...func(*DescribeScheduledInstancesPaginatorOptions)) *DescribeScheduledInstancesPaginator {
	if params == nil {
		params = &DescribeScheduledInstancesInput{}
	}

	options := DescribeScheduledInstancesPaginatorOptions{}
	if params.MaxResults != nil {
		options.Limit = *params.MaxResults
	}

	for _, fn := range optFns {
		fn(&options)
	}

	return &DescribeScheduledInstancesPaginator{
		options:   options,
		client:    client,
		params:    params,
		firstPage: true,
	}
}

// HasMorePages returns a boolean indicating whether more pages are available
func (p *DescribeScheduledInstancesPaginator) HasMorePages() bool {
	return p.firstPage || p.nextToken != nil
}

// NextPage retrieves the next DescribeScheduledInstances page.
func (p *DescribeScheduledInstancesPaginator) NextPage(ctx context.Context, optFns ...func(*Options)) (*DescribeScheduledInstancesOutput, error) {
	if !p.HasMorePages() {
		return nil, fmt.Errorf("no more pages available")
	}

	params := *p.params
	params.NextToken = p.nextToken

	var limit *int32
	if p.options.Limit > 0 {
		limit = &p.options.Limit
	}
	params.MaxResults = limit

	result, err := p.client.DescribeScheduledInstances(ctx, &params, optFns...)
	if err != nil {
		return nil, err
	}
	p.firstPage = false

	prevToken := p.nextToken
	p.nextToken = result.NextToken

	if p.options.StopOnDuplicateToken && prevToken != nil && p.nextToken != nil && *prevToken == *p.nextToken {
		p.nextToken = nil
	}

	return result, nil
}

func newServiceMetadataMiddleware_opDescribeScheduledInstances(region string) *awsmiddleware.RegisterServiceMetadata {
	return &awsmiddleware.RegisterServiceMetadata{
		Region:        region,
		ServiceID:     ServiceID,
		SigningName:   "ec2",
		OperationName: "DescribeScheduledInstances",
	}
}
