#ifndef INPUT_BRIDGE_H
#define INPUT_BRIDGE_H

#include <stdint.h>
#include <stdbool.h>

typedef struct {
    double x;
    double y;
    double width;
    double height;
} DisplayBounds;

DisplayBounds GetDisplayBounds(uint32_t displayID);
bool CheckAccessibilityPermission(void);

void InjectMouseMove(double x, double y);
void InjectLeftMouseDown(double x, double y);
void InjectLeftMouseUp(double x, double y);
void InjectLeftMouseDragged(double x, double y);
void InjectRightMouseDown(double x, double y);
void InjectRightMouseUp(double x, double y);
void InjectScrollWheel(int32_t deltaX, int32_t deltaY);

#endif
