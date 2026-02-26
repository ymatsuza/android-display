#ifndef ENCODER_BRIDGE_H
#define ENCODER_BRIDGE_H

#include <CoreVideo/CoreVideo.h>
#include <stdint.h>

typedef void (*EncodedCallback)(const uint8_t *nalData, int nalSize, int isKeyframe, int64_t timestamp, void *userData);

typedef struct {
    void *session;
    void *context; // EncoderContext pointer for cleanup
    int success;
    char errorMsg[256];
} EncoderResult;

EncoderResult CreateEncoder(int width, int height, int fps, int bitrate, EncodedCallback callback, void *userData);
int EncodeFrame(void *session, CVPixelBufferRef pixelBuffer, int64_t timestamp);
void DestroyEncoder(void *session, void *context);

#endif
