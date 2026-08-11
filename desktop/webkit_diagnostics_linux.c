#include "webkit_diagnostics_linux.h"

#include <gtk/gtk.h>
#include <webkit2/webkit2.h>

#define REASONIX_RECOVERY_COOLDOWN_US (30 * G_USEC_PER_SEC)
#ifdef REASONIX_WEBKIT_SMOKE
#define REASONIX_RECOVERY_TIMEOUT_SECONDS 5
#else
#define REASONIX_RECOVERY_TIMEOUT_SECONDS 30
#endif

static WebKitWebView *reasonix_web_view = NULL;
static gboolean reasonix_recovery_pending = FALSE;
static gboolean reasonix_recovery_load_started = FALSE;
static gboolean reasonix_recovery_load_failed = FALSE;
static gint64 reasonix_last_recovery_at = 0;
static guint reasonix_recovery_timeout_id = 0;
static guint64 reasonix_generation = 0;
static guint64 reasonix_pending_generation = 0;
static WebKitWebProcessTerminationReason reasonix_pending_reason = WEBKIT_WEB_PROCESS_CRASHED;

#ifdef REASONIX_WEBKIT_SMOKE
enum {
  REASONIX_WEBKIT_SMOKE_SUCCESS = 1,
  REASONIX_WEBKIT_SMOKE_FAILURE = 2,
  REASONIX_WEBKIT_SMOKE_TIMEOUT = 3,
  REASONIX_WEBKIT_SMOKE_COOLDOWN = 4
};
static GMainLoop *reasonix_test_loop = NULL;
static WebKitWebView *reasonix_test_web_view = NULL;
static int reasonix_test_mode = 0;
static int reasonix_test_event_count = 0;
static int reasonix_test_reload_count_value = 0;
static gboolean reasonix_test_initial_termination = FALSE;
static gboolean reasonix_test_timed_out = FALSE;
static guint reasonix_test_safety_timeout_id = 0;
#endif

static GtkWidget *reasonix_find_web_view(GtkWidget *widget) {
  if (WEBKIT_IS_WEB_VIEW(widget)) return widget;
  if (!GTK_IS_CONTAINER(widget)) return NULL;
  GList *children = gtk_container_get_children(GTK_CONTAINER(widget));
  GtkWidget *found = NULL;
  for (GList *item = children; item != NULL && found == NULL; item = item->next) {
    found = reasonix_find_web_view(GTK_WIDGET(item->data));
  }
  g_list_free(children);
  return found;
}

static void reasonix_finish_recovery(int outcome) {
  if (!reasonix_recovery_pending) return;
  reasonix_recovery_pending = FALSE;
  reasonix_recovery_load_started = FALSE;
  reasonix_recovery_load_failed = FALSE;
  if (reasonix_recovery_timeout_id != 0) {
    g_source_remove(reasonix_recovery_timeout_id);
    reasonix_recovery_timeout_id = 0;
  }
  reasonixWebKitProcessTerminated((int)reasonix_pending_reason, outcome,
                                  (unsigned long long)reasonix_pending_generation);
}

static gboolean reasonix_recovery_timeout(gpointer data) {
  (void)data;
  reasonix_recovery_timeout_id = 0;
  if (reasonix_recovery_pending) {
    reasonix_recovery_pending = FALSE;
    reasonix_recovery_load_started = FALSE;
    reasonix_recovery_load_failed = FALSE;
    reasonixWebKitProcessTerminated((int)reasonix_pending_reason, 2,
                                    (unsigned long long)reasonix_pending_generation);
  }
  return G_SOURCE_REMOVE;
}

static gboolean reasonix_reload_after_termination(gpointer data) {
  WebKitWebView *web_view = WEBKIT_WEB_VIEW(data);
  if (!reasonix_recovery_pending || web_view != reasonix_web_view) {
    return G_SOURCE_REMOVE;
  }
#ifdef REASONIX_WEBKIT_SMOKE
  reasonix_test_reload_count_value++;
#endif
  webkit_web_view_reload(web_view);
  return G_SOURCE_REMOVE;
}

static void reasonix_web_process_terminated(WebKitWebView *web_view,
                                            WebKitWebProcessTerminationReason reason,
                                            gpointer data) {
  (void)data;
  gint64 now = g_get_monotonic_time();
  guint64 generation = ++reasonix_generation;
  if (reasonix_recovery_pending ||
      (reasonix_last_recovery_at != 0 && now - reasonix_last_recovery_at < REASONIX_RECOVERY_COOLDOWN_US)) {
    reasonixWebKitProcessTerminated((int)reason, 0, (unsigned long long)generation);
    return;
  }
  reasonix_last_recovery_at = now;
  reasonix_pending_reason = reason;
  reasonix_pending_generation = generation;
  reasonix_recovery_pending = TRUE;
  reasonix_recovery_load_started = FALSE;
  reasonix_recovery_load_failed = FALSE;
  reasonix_recovery_timeout_id = g_timeout_add_seconds(REASONIX_RECOVERY_TIMEOUT_SECONDS,
                                                        reasonix_recovery_timeout, NULL);
  // Let WebKit finish dispatching the termination signal before starting a
  // replacement process. Reloading synchronously from this callback is not
  // reliable across WebKitGTK versions.
  g_idle_add_full(G_PRIORITY_DEFAULT_IDLE, reasonix_reload_after_termination,
                  g_object_ref(web_view), g_object_unref);
}

static gboolean reasonix_load_failed(WebKitWebView *web_view, WebKitLoadEvent event,
                                     const gchar *uri, GError *error, gpointer data) {
  (void)web_view;
  (void)event;
  (void)uri;
  (void)error;
  (void)data;
  // WebKit may deliver load-failed for the terminated navigation after the
  // process-terminated signal. Only failures belonging to the reload that we
  // started are recovery failures.
  if (reasonix_recovery_pending && reasonix_recovery_load_started) {
    reasonix_recovery_load_failed = TRUE;
  }
  return FALSE;
}

static void reasonix_load_changed(WebKitWebView *web_view, WebKitLoadEvent event, gpointer data) {
  (void)web_view;
  (void)data;
  if (!reasonix_recovery_pending) return;
  if (event == WEBKIT_LOAD_STARTED) {
    reasonix_recovery_load_started = TRUE;
    reasonix_recovery_load_failed = FALSE;
    return;
  }
  if (reasonix_recovery_load_started && event == WEBKIT_LOAD_FINISHED) {
#ifdef REASONIX_WEBKIT_SMOKE
    if (reasonix_test_mode == REASONIX_WEBKIT_SMOKE_TIMEOUT) return;
    if (reasonix_test_mode == REASONIX_WEBKIT_SMOKE_FAILURE) {
      reasonix_recovery_load_failed = TRUE;
    }
#endif
    reasonix_finish_recovery(reasonix_recovery_load_failed ? 2 : 1);
  }
}

static void reasonix_web_view_destroyed(GtkWidget *widget, gpointer data) {
  (void)widget;
  (void)data;
  if (reasonix_recovery_pending) reasonix_finish_recovery(2);
  reasonix_web_view = NULL;
}

static gboolean reasonix_attach_webkit_observer(gpointer data) {
  (void)data;
  if (reasonix_web_view != NULL) return G_SOURCE_REMOVE;
  GList *windows = gtk_window_list_toplevels();
  GtkWidget *found = NULL;
  for (GList *item = windows; item != NULL && found == NULL; item = item->next) {
    found = reasonix_find_web_view(GTK_WIDGET(item->data));
  }
  g_list_free(windows);
  if (found == NULL) return G_SOURCE_REMOVE;
  if (g_signal_lookup("web-process-terminated", WEBKIT_TYPE_WEB_VIEW) == 0) {
    return G_SOURCE_REMOVE;
  }

  WebKitWebView *web_view = WEBKIT_WEB_VIEW(found);
  if (g_signal_connect(web_view, "web-process-terminated",
                       G_CALLBACK(reasonix_web_process_terminated), NULL) == 0) {
    return G_SOURCE_REMOVE;
  }
  reasonix_web_view = web_view;
  g_signal_connect(reasonix_web_view, "load-failed", G_CALLBACK(reasonix_load_failed), NULL);
  g_signal_connect(reasonix_web_view, "load-changed", G_CALLBACK(reasonix_load_changed), NULL);
  g_signal_connect(reasonix_web_view, "destroy", G_CALLBACK(reasonix_web_view_destroyed), NULL);

  WebKitSettings *settings = webkit_web_view_get_settings(reasonix_web_view);
  WebKitHardwareAccelerationPolicy policy = WEBKIT_HARDWARE_ACCELERATION_POLICY_ON_DEMAND;
  if (settings != NULL) policy = webkit_settings_get_hardware_acceleration_policy(settings);
  reasonixWebKitRuntimeReady((int)webkit_get_major_version(), (int)webkit_get_minor_version(),
                            (int)webkit_get_micro_version(), (int)policy);
  return G_SOURCE_REMOVE;
}

void reasonix_install_webkit_observer(void) {
  g_main_context_invoke(NULL, reasonix_attach_webkit_observer, NULL);
}

#ifdef REASONIX_WEBKIT_SMOKE
static gboolean reasonix_test_terminate_again(gpointer data) {
  (void)data;
  if (reasonix_test_web_view != NULL) {
    webkit_web_view_terminate_web_process(reasonix_test_web_view);
  }
  return G_SOURCE_REMOVE;
}

static void reasonix_test_initial_load_changed(WebKitWebView *web_view,
                                                WebKitLoadEvent event,
                                                gpointer data) {
  (void)data;
  if (event != WEBKIT_LOAD_FINISHED || reasonix_test_initial_termination) return;
  reasonix_test_initial_termination = TRUE;
  webkit_web_view_terminate_web_process(web_view);
}

static gboolean reasonix_test_safety_timeout(gpointer data) {
  (void)data;
  reasonix_test_safety_timeout_id = 0;
  reasonix_test_timed_out = TRUE;
  if (reasonix_test_loop != NULL) g_main_loop_quit(reasonix_test_loop);
  return G_SOURCE_REMOVE;
}

void reasonix_test_webkit_event_seen(int reason, int recovery) {
  (void)reason;
  reasonix_test_event_count++;
  if (reasonix_test_mode == REASONIX_WEBKIT_SMOKE_COOLDOWN &&
      reasonix_test_event_count == 1 && recovery == 1) {
    g_idle_add(reasonix_test_terminate_again, NULL);
    return;
  }
  if (reasonix_test_loop != NULL) g_main_loop_quit(reasonix_test_loop);
}

int reasonix_test_webkit_reload_count(void) {
  return reasonix_test_reload_count_value;
}

int reasonix_test_webkit_run(int mode) {
  if (mode < REASONIX_WEBKIT_SMOKE_SUCCESS || mode > REASONIX_WEBKIT_SMOKE_COOLDOWN) return -1;
  if (!gtk_init_check(NULL, NULL)) return -2;

  if (reasonix_recovery_timeout_id != 0) {
    g_source_remove(reasonix_recovery_timeout_id);
    reasonix_recovery_timeout_id = 0;
  }
  reasonix_web_view = NULL;
  reasonix_recovery_pending = FALSE;
  reasonix_recovery_load_started = FALSE;
  reasonix_recovery_load_failed = FALSE;
  reasonix_last_recovery_at = 0;
  reasonix_generation = 0;
  reasonix_pending_generation = 0;
  reasonix_test_mode = mode;
  reasonix_test_event_count = 0;
  reasonix_test_reload_count_value = 0;
  reasonix_test_initial_termination = FALSE;
  reasonix_test_timed_out = FALSE;

  GtkWidget *window = gtk_window_new(GTK_WINDOW_TOPLEVEL);
  GtkWidget *widget = webkit_web_view_new();
  if (window == NULL || widget == NULL) return -3;
  gtk_container_add(GTK_CONTAINER(window), widget);
  gtk_widget_show_all(window);
  if (reasonix_attach_webkit_observer(NULL) != G_SOURCE_REMOVE || reasonix_web_view == NULL) {
    gtk_widget_destroy(window);
    return -4;
  }
  reasonix_test_web_view = WEBKIT_WEB_VIEW(widget);
  g_signal_connect(widget, "load-changed", G_CALLBACK(reasonix_test_initial_load_changed), NULL);
  reasonix_test_loop = g_main_loop_new(NULL, FALSE);
  reasonix_test_safety_timeout_id = g_timeout_add_seconds(15, reasonix_test_safety_timeout, NULL);
  webkit_web_view_load_uri(
      reasonix_test_web_view,
      "data:text/html,%3Chtml%3E%3Cbody%3EReasonix%20WebKit%20native%20smoke%3C%2Fbody%3E%3C%2Fhtml%3E");
  g_main_loop_run(reasonix_test_loop);

  if (reasonix_test_safety_timeout_id != 0) {
    g_source_remove(reasonix_test_safety_timeout_id);
    reasonix_test_safety_timeout_id = 0;
  }
  g_main_loop_unref(reasonix_test_loop);
  reasonix_test_loop = NULL;
  gtk_widget_destroy(window);
  reasonix_test_web_view = NULL;
  reasonix_web_view = NULL;
  return reasonix_test_timed_out ? -5 : 0;
}
#endif
