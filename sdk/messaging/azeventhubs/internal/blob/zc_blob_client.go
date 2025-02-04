//go:build go1.18
// +build go1.18

// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package blob

import (
	"context"
	"errors"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/runtime"
)

// BlobClient represents a URL to an Azure Storage blob; the blob may be a block blob, append blob, or page blob.
type BlobClient struct {
	client    *blobClient
	sharedKey *SharedKeyCredential
}

// NewBlobClient creates a BlobClient object using the specified URL, Azure AD credential, and options.
func NewBlobClient(blobURL string, cred azcore.TokenCredential, options *ClientOptions) (*BlobClient, error) {
	authPolicy := runtime.NewBearerTokenPolicy(cred, []string{tokenScope}, nil)
	conOptions := getConnectionOptions(options)
	conOptions.PerRetryPolicies = append(conOptions.PerRetryPolicies, authPolicy)
	conn := newConnection(blobURL, conOptions)

	return &BlobClient{
		client: newBlobClient(conn.Endpoint(), conn.Pipeline()),
	}, nil
}

// NewBlobClientWithNoCredential creates a BlobClient object using the specified URL and options.
func NewBlobClientWithNoCredential(blobURL string, options *ClientOptions) (*BlobClient, error) {
	conOptions := getConnectionOptions(options)
	conn := newConnection(blobURL, conOptions)

	return &BlobClient{
		client: newBlobClient(conn.Endpoint(), conn.Pipeline()),
	}, nil
}

// NewBlobClientWithSharedKey creates a BlobClient object using the specified URL, shared key, and options.
func NewBlobClientWithSharedKey(blobURL string, cred *SharedKeyCredential, options *ClientOptions) (*BlobClient, error) {
	authPolicy := newSharedKeyCredPolicy(cred)
	conOptions := getConnectionOptions(options)
	conOptions.PerRetryPolicies = append(conOptions.PerRetryPolicies, authPolicy)
	conn := newConnection(blobURL, conOptions)

	return &BlobClient{
		client:    newBlobClient(blobURL, conn.Pipeline()),
		sharedKey: cred,
	}, nil
}

// NewBlobClientFromConnectionString creates BlobClient from a connection String
// nolint
func NewBlobClientFromConnectionString(connectionString, containerName, blobName string, options *ClientOptions) (*BlobClient, error) {
	containerClient, err := NewContainerClientFromConnectionString(connectionString, containerName, options)
	if err != nil {
		return nil, err
	}
	return containerClient.NewBlobClient(blobName)
}

// URL returns the URL endpoint used by the BlobClient object.
func (b *BlobClient) URL() string {
	return b.client.endpoint
}

// WithSnapshot creates a new BlobClient object identical to the source but with the specified snapshot timestamp.
// Pass "" to remove the snapshot returning a URL to the base blob.
func (b *BlobClient) WithSnapshot(snapshot string) (*BlobClient, error) {
	p, err := NewBlobURLParts(b.URL())
	if err != nil {
		return nil, err
	}
	p.Snapshot = snapshot

	pipeline := b.client.pl
	return &BlobClient{
		client:    newBlobClient(p.URL(), pipeline),
		sharedKey: b.sharedKey,
	}, nil
}

// WithVersionID creates a new AppendBlobURL object identical to the source but with the specified version id.
// Pass "" to remove the versionID returning a URL to the base blob.
func (b *BlobClient) WithVersionID(versionID string) (*BlobClient, error) {
	p, err := NewBlobURLParts(b.URL())
	if err != nil {
		return nil, err
	}
	p.VersionID = versionID

	pipeline := b.client.pl
	return &BlobClient{
		client:    newBlobClient(p.URL(), pipeline),
		sharedKey: b.sharedKey,
	}, nil
}

// Download reads a range of bytes from a blob. The response also includes the blob's properties and metadata.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/get-blob.
func (b *BlobClient) Download(ctx context.Context, options *BlobDownloadOptions) (BlobDownloadResponse, error) {
	o, lease, cpk, accessConditions := options.format()
	dr, err := b.client.Download(ctx, o, lease, cpk, accessConditions)
	if err != nil {
		return BlobDownloadResponse{}, handleError(err)
	}

	offset := int64(0)
	count := int64(CountToEnd)

	if options != nil && options.Offset != nil {
		offset = *options.Offset
	}

	if options != nil && options.Count != nil {
		count = *options.Count
	}

	eTag := ""
	if dr.ETag != nil {
		eTag = *dr.ETag
	}
	return BlobDownloadResponse{
		b:                          b,
		blobClientDownloadResponse: dr,
		ctx:                        ctx,
		getInfo:                    HTTPGetterInfo{Offset: offset, Count: count, ETag: eTag},
		ObjectReplicationRules:     deserializeORSPolicies(dr.ObjectReplicationRules),
	}, err
}

// Delete marks the specified blob or snapshot for deletion. The blob is later deleted during garbage collection.
// Note that deleting a blob also deletes all its snapshots.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/delete-blob.
func (b *BlobClient) Delete(ctx context.Context, o *BlobDeleteOptions) (BlobDeleteResponse, error) {
	basics, leaseInfo, accessConditions := o.format()
	resp, err := b.client.Delete(ctx, basics, leaseInfo, accessConditions)

	return toBlobDeleteResponse(resp), handleError(err)
}

// Undelete restores the contents and metadata of a soft-deleted blob and any associated soft-deleted snapshots.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/undelete-blob.
func (b *BlobClient) Undelete(ctx context.Context, o *BlobUndeleteOptions) (BlobUndeleteResponse, error) {
	undeleteOptions := o.format()
	resp, err := b.client.Undelete(ctx, undeleteOptions)

	return toBlobUndeleteResponse(resp), handleError(err)
}

// SetTier operation sets the tier on a blob. The operation is allowed on a page
// blob in a premium storage account and on a block blob in a blob storage account (locally
// redundant storage only). A premium page blob's tier determines the allowed size, IOPS, and
// bandwidth of the blob. A block blob's tier determines Hot/Cool/Archive storage type. This operation
// does not update the blob's ETag.
// For detailed information about block blob level tiering see https://docs.microsoft.com/en-us/azure/storage/blobs/storage-blob-storage-tiers.
func (b *BlobClient) SetTier(ctx context.Context, tier AccessTier, options *BlobSetTierOptions) (BlobSetTierResponse, error) {
	basics, lease, accessConditions := options.format()
	resp, err := b.client.SetTier(ctx, tier, basics, lease, accessConditions)

	return toBlobSetTierResponse(resp), handleError(err)
}

// GetProperties returns the blob's properties.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/get-blob-properties.
func (b *BlobClient) GetProperties(ctx context.Context, options *BlobGetPropertiesOptions) (BlobGetPropertiesResponse, error) {
	basics, lease, cpk, access := options.format()
	resp, err := b.client.GetProperties(ctx, basics, lease, cpk, access)

	return toGetBlobPropertiesResponse(resp), handleError(err)
}

// SetHTTPHeaders changes a blob's HTTP headers.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/set-blob-properties.
func (b *BlobClient) SetHTTPHeaders(ctx context.Context, blobHttpHeaders BlobHTTPHeaders, options *BlobSetHTTPHeadersOptions) (BlobSetHTTPHeadersResponse, error) {
	basics, lease, access := options.format()
	resp, err := b.client.SetHTTPHeaders(ctx, basics, &blobHttpHeaders, lease, access)

	return toBlobSetHTTPHeadersResponse(resp), handleError(err)
}

// SetMetadata changes a blob's metadata.
// https://docs.microsoft.com/rest/api/storageservices/set-blob-metadata.
func (b *BlobClient) SetMetadata(ctx context.Context, metadata map[string]string, options *BlobSetMetadataOptions) (BlobSetMetadataResponse, error) {
	basics := blobClientSetMetadataOptions{
		Metadata: metadata,
	}
	lease, cpk, cpkScope, access := options.format()
	resp, err := b.client.SetMetadata(ctx, &basics, lease, cpk, cpkScope, access)

	return toBlobSetMetadataResponse(resp), handleError(err)
}

// CreateSnapshot creates a read-only snapshot of a blob.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/snapshot-blob.
func (b *BlobClient) CreateSnapshot(ctx context.Context, options *BlobCreateSnapshotOptions) (BlobCreateSnapshotResponse, error) {
	// CreateSnapshot does NOT panic if the user tries to create a snapshot using a URL that already has a snapshot query parameter
	// because checking this would be a performance hit for a VERY unusual path, and we don't think the common case should suffer this
	// performance hit.
	basics, cpk, cpkScope, access, lease := options.format()
	resp, err := b.client.CreateSnapshot(ctx, basics, cpk, cpkScope, access, lease)

	return toBlobCreateSnapshotResponse(resp), handleError(err)
}

// StartCopyFromURL copies the data at the source URL to a blob.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/copy-blob.
func (b *BlobClient) StartCopyFromURL(ctx context.Context, copySource string, options *BlobStartCopyOptions) (BlobStartCopyFromURLResponse, error) {
	basics, srcAccess, destAccess, lease := options.format()
	resp, err := b.client.StartCopyFromURL(ctx, copySource, basics, srcAccess, destAccess, lease)

	return toBlobStartCopyFromURLResponse(resp), handleError(err)
}

// AbortCopyFromURL stops a pending copy that was previously started and leaves a destination blob with 0 length and metadata.
// For more information, see https://docs.microsoft.com/rest/api/storageservices/abort-copy-blob.
func (b *BlobClient) AbortCopyFromURL(ctx context.Context, copyID string, options *BlobAbortCopyOptions) (BlobAbortCopyFromURLResponse, error) {
	basics, lease := options.format()
	resp, err := b.client.AbortCopyFromURL(ctx, copyID, basics, lease)

	return toBlobAbortCopyFromURLResponse(resp), handleError(err)
}

// SetTags operation enables users to set tags on a blob or specific blob version, but not snapshot.
// Each call to this operation replaces all existing tags attached to the blob.
// To remove all tags from the blob, call this operation with no tags set.
// https://docs.microsoft.com/en-us/rest/api/storageservices/set-blob-tags
func (b *BlobClient) SetTags(ctx context.Context, options *BlobSetTagsOptions) (BlobSetTagsResponse, error) {
	blobSetTagsOptions, modifiedAccessConditions, leaseAccessConditions := options.format()
	resp, err := b.client.SetTags(ctx, blobSetTagsOptions, modifiedAccessConditions, leaseAccessConditions)

	return toBlobSetTagsResponse(resp), handleError(err)
}

// GetTags operation enables users to get tags on a blob or specific blob version, or snapshot.
// https://docs.microsoft.com/en-us/rest/api/storageservices/get-blob-tags
func (b *BlobClient) GetTags(ctx context.Context, options *BlobGetTagsOptions) (BlobGetTagsResponse, error) {
	blobGetTagsOptions, modifiedAccessConditions, leaseAccessConditions := options.format()
	resp, err := b.client.GetTags(ctx, blobGetTagsOptions, modifiedAccessConditions, leaseAccessConditions)

	return toBlobGetTagsResponse(resp), handleError(err)

}

// GetSASToken is a convenience method for generating a SAS token for the currently pointed at blob.
// It can only be used if the credential supplied during creation was a SharedKeyCredential.
func (b *BlobClient) GetSASToken(permissions BlobSASPermissions, start time.Time, expiry time.Time) (SASQueryParameters, error) {
	urlParts, _ := NewBlobURLParts(b.URL())

	t, err := time.Parse(SnapshotTimeFormat, urlParts.Snapshot)

	if err != nil {
		t = time.Time{}
	}

	if b.sharedKey == nil {
		return SASQueryParameters{}, errors.New("credential is not a SharedKeyCredential. SAS can only be signed with a SharedKeyCredential")
	}

	return BlobSASSignatureValues{
		ContainerName: urlParts.ContainerName,
		BlobName:      urlParts.BlobName,
		SnapshotTime:  t,
		Version:       SASVersion,

		Permissions: permissions.String(),

		StartTime:  start.UTC(),
		ExpiryTime: expiry.UTC(),
	}.NewSASQueryParameters(b.sharedKey)
}
