//go:build darwin

package main

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa

#import <Cocoa/Cocoa.h>

// Dock progress: macOS has no first-party progress API, so — like mpv and
// Electron — the app icon is redrawn with a progress bar laid over it via the
// dock tile's content view. Clearing restores the plain icon.

static NSProgressIndicator *gDockProgress = nil;

static void setDockProgressOnMain(double fraction) {
	NSDockTile *tile = [NSApp dockTile];
	if (fraction < 0) {
		if (gDockProgress != nil) {
			gDockProgress = nil;
			[tile setContentView:nil];
			[tile display];
		}
		return;
	}
	if (gDockProgress == nil) {
		NSImageView *icon = [[NSImageView alloc]
			initWithFrame:NSMakeRect(0, 0, tile.size.width, tile.size.height)];
		[icon setImage:[NSApp applicationIconImage]];
		[tile setContentView:icon];

		NSProgressIndicator *bar = [[NSProgressIndicator alloc]
			initWithFrame:NSMakeRect(6.0, 6.0, tile.size.width - 12.0, 12.0)];
		[bar setStyle:NSProgressIndicatorStyleBar];
		[bar setIndeterminate:NO];
		[bar setMinValue:0.0];
		[bar setMaxValue:1.0];
		[icon addSubview:bar];
		gDockProgress = bar;
	}
	[gDockProgress setDoubleValue:fraction];
	[tile display];
}

static void setDockProgress(double fraction) {
	if ([NSThread isMainThread]) {
		setDockProgressOnMain(fraction);
	} else {
		dispatch_async(dispatch_get_main_queue(), ^{
			setDockProgressOnMain(fraction);
		});
	}
}
*/
import "C"

// setTaskbarProgress shows overall progress on the app's dock icon: 0..1
// draws a progress bar over the icon, a negative value restores the plain
// icon. Safe to call from any goroutine (work hops to the main thread).
func setTaskbarProgress(f float64) {
	C.setDockProgress(C.double(f))
}
