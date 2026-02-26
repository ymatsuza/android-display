#ifndef DISPLAY_BRIDGE_H
#define DISPLAY_BRIDGE_H

#include <CoreGraphics/CoreGraphics.h>

typedef struct {
    int width;
    int height;
    int ppi;
    int hiDPI;
} VirtualDisplayConfig;

typedef struct {
    void *display;
    CGDirectDisplayID displayID;
    int success;
    char errorMsg[256];
} VirtualDisplayResult;

VirtualDisplayResult CreateVirtualDisplay(VirtualDisplayConfig config);
void DestroyVirtualDisplay(void *display);
CGDirectDisplayID GetVirtualDisplayID(void *display);

#endif
