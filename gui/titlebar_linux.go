//go:build linux

package main

/*
#cgo pkg-config: gtk+-3.0
#include <gtk/gtk.h>

// GTK draws the window's title bar itself (client-side decoration) and GNOME
// themes size that default bar at ~37px, noticeably taller than the bars of
// browsers and Electron apps. Override the theme's sizing to bring it down to
// ~29px while keeping the native buttons and behaviour.
static const char *slimTitleBarCSS =
	"headerbar.default-decoration { min-height: 0; padding: 2px 6px; }\n"
	"headerbar.default-decoration button.titlebutton {"
	" min-height: 22px; min-width: 22px; padding: 0; margin: 0; }\n";

static gboolean installSlimTitleBar(gpointer data) {
	GtkCssProvider *provider = gtk_css_provider_new();
	gtk_css_provider_load_from_data(provider, slimTitleBarCSS, -1, NULL);
	gtk_style_context_add_provider_for_screen(gdk_screen_get_default(),
		GTK_STYLE_PROVIDER(provider), GTK_STYLE_PROVIDER_PRIORITY_APPLICATION);
	g_object_unref(provider);
	return G_SOURCE_REMOVE;
}

static void scheduleSlimTitleBar(void) {
	// Wails runs OnStartup off the GTK main thread; g_idle_add is thread-safe
	// and runs the callback on it.
	g_idle_add(installSlimTitleBar, NULL);
}
*/
import "C"

// slimTitleBar shrinks the GTK default title bar to match other desktop apps.
func slimTitleBar() { C.scheduleSlimTitleBar() }
