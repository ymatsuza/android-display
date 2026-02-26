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

void InjectTabletProximityEnter(void) {
    CGEventRef event = CGEventCreate(NULL);
    if (event) {
        CGEventSetType(event, kCGEventTabletProximity);
        CGEventSetIntegerValueField(event, kCGTabletProximityEventVendorID, 0x056A);
        CGEventSetIntegerValueField(event, kCGTabletProximityEventTabletID, 1);
        CGEventSetIntegerValueField(event, kCGTabletProximityEventPointerID, 1);
        CGEventSetIntegerValueField(event, kCGTabletProximityEventDeviceID, 1);
        CGEventSetIntegerValueField(event, kCGTabletProximityEventSystemTabletID, 1);
        CGEventSetIntegerValueField(event, kCGTabletProximityEventVendorPointerType, 1);
        CGEventSetIntegerValueField(event, kCGTabletProximityEventVendorPointerSerialNumber, 1);
        CGEventSetIntegerValueField(event, kCGTabletProximityEventVendorUniqueID, 1);
        CGEventSetIntegerValueField(event, kCGTabletProximityEventCapabilityMask, 0x00FE);
        CGEventSetIntegerValueField(event, kCGTabletProximityEventPointerType, 1);
        CGEventSetIntegerValueField(event, kCGTabletProximityEventEnterProximity, 1);
        CGEventPost(kCGHIDEventTap, event);
        CFRelease(event);
    }
}

void InjectTabletProximityLeave(void) {
    CGEventRef event = CGEventCreate(NULL);
    if (event) {
        CGEventSetType(event, kCGEventTabletProximity);
        CGEventSetIntegerValueField(event, kCGTabletProximityEventVendorID, 0x056A);
        CGEventSetIntegerValueField(event, kCGTabletProximityEventTabletID, 1);
        CGEventSetIntegerValueField(event, kCGTabletProximityEventPointerID, 1);
        CGEventSetIntegerValueField(event, kCGTabletProximityEventDeviceID, 1);
        CGEventSetIntegerValueField(event, kCGTabletProximityEventSystemTabletID, 1);
        CGEventSetIntegerValueField(event, kCGTabletProximityEventVendorPointerType, 1);
        CGEventSetIntegerValueField(event, kCGTabletProximityEventVendorPointerSerialNumber, 1);
        CGEventSetIntegerValueField(event, kCGTabletProximityEventVendorUniqueID, 1);
        CGEventSetIntegerValueField(event, kCGTabletProximityEventCapabilityMask, 0x00FE);
        CGEventSetIntegerValueField(event, kCGTabletProximityEventPointerType, 1);
        CGEventSetIntegerValueField(event, kCGTabletProximityEventEnterProximity, 0);
        CGEventPost(kCGHIDEventTap, event);
        CFRelease(event);
    }
}

static void InjectTabletEvent(CGEventType eventType, double x, double y, double pressure, double tiltX, double tiltY) {
    CGEventRef event = CGEventCreateMouseEvent(NULL, eventType, CGPointMake(x, y), kCGMouseButtonLeft);
    if (event) {
        CGEventSetIntegerValueField(event, kCGMouseEventSubtype, kCGEventMouseSubtypeTabletPoint);
        CGEventSetIntegerValueField(event, kCGTabletEventPointPressure, (int64_t)(pressure * 65535.0));
        CGEventSetDoubleValueField(event, kCGTabletEventTiltX, tiltX);
        CGEventSetDoubleValueField(event, kCGTabletEventTiltY, tiltY);
        CGEventPost(kCGHIDEventTap, event);
        CFRelease(event);
    }
}

void InjectTabletDown(double x, double y, double pressure, double tiltX, double tiltY) {
    InjectTabletEvent(kCGEventLeftMouseDown, x, y, pressure, tiltX, tiltY);
}

void InjectTabletUp(double x, double y, double pressure, double tiltX, double tiltY) {
    InjectTabletEvent(kCGEventLeftMouseUp, x, y, pressure, tiltX, tiltY);
}

void InjectTabletDragged(double x, double y, double pressure, double tiltX, double tiltY) {
    InjectTabletEvent(kCGEventLeftMouseDragged, x, y, pressure, tiltX, tiltY);
}

void InjectTabletMoved(double x, double y, double pressure, double tiltX, double tiltY) {
    InjectTabletEvent(kCGEventMouseMoved, x, y, pressure, tiltX, tiltY);
}
