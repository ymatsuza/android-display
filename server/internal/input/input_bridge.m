#import <CoreGraphics/CoreGraphics.h>
#import <ApplicationServices/ApplicationServices.h>
#include "input_bridge.h"

DisplayBounds GetDisplayBounds(uint32_t displayID) {
    CGRect rect = CGDisplayBounds(displayID);
    DisplayBounds bounds;
    bounds.x = rect.origin.x;
    bounds.y = rect.origin.y;
    bounds.width = rect.size.width;
    bounds.height = rect.size.height;
    return bounds;
}

bool CheckAccessibilityPermission(void) {
    return AXIsProcessTrusted();
}

void InjectMouseMove(double x, double y) {
    CGEventRef event = CGEventCreateMouseEvent(NULL, kCGEventMouseMoved, CGPointMake(x, y), kCGMouseButtonLeft);
    if (event) {
        CGEventPost(kCGHIDEventTap, event);
        CFRelease(event);
    }
}

void InjectLeftMouseDown(double x, double y) {
    CGEventRef event = CGEventCreateMouseEvent(NULL, kCGEventLeftMouseDown, CGPointMake(x, y), kCGMouseButtonLeft);
    if (event) {
        CGEventPost(kCGHIDEventTap, event);
        CFRelease(event);
    }
}

void InjectLeftMouseUp(double x, double y) {
    CGEventRef event = CGEventCreateMouseEvent(NULL, kCGEventLeftMouseUp, CGPointMake(x, y), kCGMouseButtonLeft);
    if (event) {
        CGEventPost(kCGHIDEventTap, event);
        CFRelease(event);
    }
}

void InjectLeftMouseDragged(double x, double y) {
    CGEventRef event = CGEventCreateMouseEvent(NULL, kCGEventLeftMouseDragged, CGPointMake(x, y), kCGMouseButtonLeft);
    if (event) {
        CGEventPost(kCGHIDEventTap, event);
        CFRelease(event);
    }
}

void InjectRightMouseDown(double x, double y) {
    CGEventRef event = CGEventCreateMouseEvent(NULL, kCGEventRightMouseDown, CGPointMake(x, y), kCGMouseButtonRight);
    if (event) {
        CGEventPost(kCGHIDEventTap, event);
        CFRelease(event);
    }
}

void InjectRightMouseUp(double x, double y) {
    CGEventRef event = CGEventCreateMouseEvent(NULL, kCGEventRightMouseUp, CGPointMake(x, y), kCGMouseButtonRight);
    if (event) {
        CGEventPost(kCGHIDEventTap, event);
        CFRelease(event);
    }
}

void InjectScrollWheel(int32_t deltaX, int32_t deltaY) {
    CGEventRef event = CGEventCreateScrollWheelEvent(NULL, kCGScrollEventUnitPixel, 2, deltaY, deltaX);
    if (event) {
        CGEventPost(kCGHIDEventTap, event);
        CFRelease(event);
    }
}
