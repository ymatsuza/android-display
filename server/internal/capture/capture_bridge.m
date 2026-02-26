#import <Foundation/Foundation.h>
#import <CoreGraphics/CoreGraphics.h>
#import <CoreMedia/CoreMedia.h>
#import <CoreVideo/CoreVideo.h>
#import <objc/runtime.h>
#include "capture_bridge.h"
#include <string.h>

#if __has_include(<ScreenCaptureKit/ScreenCaptureKit.h>)
#import <ScreenCaptureKit/ScreenCaptureKit.h>
#define HAS_SCREENCAPTUREKIT 1
#else
#define HAS_SCREENCAPTUREKIT 0
#endif

#if HAS_SCREENCAPTUREKIT

// FrameHandler acts as the SCStreamOutput delegate, receiving captured frames
// from ScreenCaptureKit and forwarding them to the Go callback.
API_AVAILABLE(macos(12.3))
@interface FrameHandler : NSObject <SCStreamOutput>
@property (nonatomic, assign) FrameCallback callback;
@property (nonatomic, assign) void *userData;
@end

API_AVAILABLE(macos(12.3))
@implementation FrameHandler

- (void)stream:(SCStream *)stream
    didOutputSampleBuffer:(CMSampleBufferRef)sampleBuffer
                   ofType:(SCStreamOutputType)type {
    if (type != SCStreamOutputTypeScreen) {
        return;
    }

    // Extract the pixel buffer from the sample buffer
    CVPixelBufferRef pixelBuffer = CMSampleBufferGetImageBuffer(sampleBuffer);
    if (!pixelBuffer) {
        return;
    }

    int width = (int)CVPixelBufferGetWidth(pixelBuffer);
    int height = (int)CVPixelBufferGetHeight(pixelBuffer);

    // Get the presentation timestamp in microseconds
    CMTime pts = CMSampleBufferGetPresentationTimeStamp(sampleBuffer);
    int64_t timestamp = 0;
    if (CMTIME_IS_VALID(pts)) {
        timestamp = (int64_t)(CMTimeGetSeconds(pts) * 1000000.0);
    }

    // Retain the pixel buffer so Go side can use it asynchronously.
    // The Go side is responsible for calling ReleasePixelBuffer.
    CVPixelBufferRetain(pixelBuffer);

    if (self.callback) {
        self.callback((void *)pixelBuffer, width, height, timestamp, self.userData);
    }
}

@end

#endif // HAS_SCREENCAPTUREKIT

CaptureResult StartCapture(CGDirectDisplayID displayID, int fps, FrameCallback callback, void *userData) {
    CaptureResult result;
    memset(&result, 0, sizeof(result));

#if HAS_SCREENCAPTUREKIT
    if (@available(macOS 12.3, *)) {
        __block SCStream *captureStream = nil;
        __block NSString *errorMessage = nil;
        __block BOOL completed = NO;

        dispatch_semaphore_t semaphore = dispatch_semaphore_create(0);

        // Step 1: Get shareable content to find the target display
        [SCShareableContent getShareableContentWithCompletionHandler:^(SCShareableContent * _Nullable content, NSError * _Nullable error) {
            if (error || !content) {
                errorMessage = error ? [error localizedDescription] : @"failed to get shareable content";
                dispatch_semaphore_signal(semaphore);
                return;
            }

            // Step 2: Find the matching display
            SCDisplay *targetDisplay = nil;
            for (SCDisplay *display in content.displays) {
                if (display.displayID == displayID) {
                    targetDisplay = display;
                    break;
                }
            }

            if (!targetDisplay) {
                errorMessage = [NSString stringWithFormat:@"display %u not found", displayID];
                dispatch_semaphore_signal(semaphore);
                return;
            }

            // Step 3: Create content filter for the display (capture entire display)
            SCContentFilter *filter = [[SCContentFilter alloc]
                initWithDisplay:targetDisplay
                excludingWindows:@[]];

            // Step 4: Configure the stream
            SCStreamConfiguration *config = [[SCStreamConfiguration alloc] init];
            config.width = targetDisplay.width;
            config.height = targetDisplay.height;
            config.minimumFrameInterval = CMTimeMake(1, fps);
            config.pixelFormat = kCVPixelFormatType_32BGRA;
            config.showsCursor = YES;

            // Step 5: Create and configure stream
            SCStream *stream = [[SCStream alloc] initWithFilter:filter
                                                  configuration:config
                                                       delegate:nil];

            // Step 6: Create frame handler
            FrameHandler *handler = [[FrameHandler alloc] init];
            handler.callback = callback;
            handler.userData = userData;

            NSError *addOutputError = nil;
            BOOL added = [stream addStreamOutput:handler
                                            type:SCStreamOutputTypeScreen
                                  sampleHandlerQueue:dispatch_get_global_queue(DISPATCH_QUEUE_PRIORITY_HIGH, 0)
                                           error:&addOutputError];
            if (!added || addOutputError) {
                errorMessage = addOutputError ? [addOutputError localizedDescription] : @"failed to add stream output";
                dispatch_semaphore_signal(semaphore);
                return;
            }

            // Step 7: Start capture
            [stream startCaptureWithCompletionHandler:^(NSError * _Nullable startError) {
                if (startError) {
                    errorMessage = [startError localizedDescription];
                } else {
                    captureStream = stream;
                    completed = YES;

                    // Prevent stream and handler from being released while active
                    // by associating the handler with the stream using objc_setAssociatedObject.
                    objc_setAssociatedObject(stream, "frameHandler", handler, OBJC_ASSOCIATION_RETAIN_NONATOMIC);
                }
                dispatch_semaphore_signal(semaphore);
            }];
        }];

        // Wait for async operations with a 5 second timeout
        long timeout = dispatch_semaphore_wait(semaphore, dispatch_time(DISPATCH_TIME_NOW, 5 * NSEC_PER_SEC));
        if (timeout != 0) {
            snprintf(result.errorMsg, sizeof(result.errorMsg), "capture start timed out");
            return result;
        }

        if (errorMessage) {
            snprintf(result.errorMsg, sizeof(result.errorMsg), "%s",
                     [errorMessage UTF8String]);
            return result;
        }

        if (!completed || !captureStream) {
            snprintf(result.errorMsg, sizeof(result.errorMsg), "capture start failed");
            return result;
        }

        // Transfer ownership to C side
        result.stream = (__bridge_retained void *)captureStream;
        result.success = 1;
    } else {
        snprintf(result.errorMsg, sizeof(result.errorMsg), "macOS 12.3+ required for ScreenCaptureKit");
    }
#else
    snprintf(result.errorMsg, sizeof(result.errorMsg), "ScreenCaptureKit not available (macOS 12.3+ required)");
#endif

    return result;
}

void StopCapture(void *stream) {
    if (stream == NULL) {
        return;
    }

#if HAS_SCREENCAPTUREKIT
    if (@available(macOS 12.3, *)) {
        @autoreleasepool {
            SCStream *captureStream = (__bridge_transfer SCStream *)stream;

            dispatch_semaphore_t semaphore = dispatch_semaphore_create(0);

            [captureStream stopCaptureWithCompletionHandler:^(NSError * _Nullable error) {
                // Ignore errors during stop -- we're shutting down
                dispatch_semaphore_signal(semaphore);
            }];

            // Wait up to 3 seconds for stop to complete
            dispatch_semaphore_wait(semaphore, dispatch_time(DISPATCH_TIME_NOW, 3 * NSEC_PER_SEC));

            // Remove the associated frame handler so it gets released
            objc_setAssociatedObject(captureStream, "frameHandler", nil, OBJC_ASSOCIATION_RETAIN_NONATOMIC);
        }
    }
#endif
}

void ReleasePixelBuffer(void *pixelBuffer) {
    if (pixelBuffer != NULL) {
        CVPixelBufferRelease((CVPixelBufferRef)pixelBuffer);
    }
}
