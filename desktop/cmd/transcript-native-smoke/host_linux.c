#ifdef REASONIX_TRANSCRIPT_SMOKE

#include <gtk/gtk.h>
#include <string.h>
#include <webkit2/webkit2.h>

typedef struct {
  GtkWidget *window;
  WebKitWebView *web_view;
  GMainLoop *loop;
  char *result;
  guint wheel_source;
  guint safety_source;
  guint wheel_tick;
  gboolean done;
} ReasonixTranscriptSmokeHost;

static void reasonix_transcript_finish(ReasonixTranscriptSmokeHost *host, const char *result) {
  if (host->done) return;
  host->done = TRUE;
  if (host->wheel_source != 0) {
    g_source_remove(host->wheel_source);
    host->wheel_source = 0;
  }
  if (host->safety_source != 0) {
    g_source_remove(host->safety_source);
    host->safety_source = 0;
  }
  host->result = g_strdup(result);
  g_main_loop_quit(host->loop);
}

static void reasonix_transcript_run_js(ReasonixTranscriptSmokeHost *host, const char *script) {
  webkit_web_view_run_javascript(host->web_view, script, NULL, NULL, NULL);
}

static gboolean reasonix_transcript_request_result(gpointer data) {
  ReasonixTranscriptSmokeHost *host = data;
  reasonix_transcript_run_js(host, "window.__reasonixNativeTranscriptSmoke.finish()");
  return G_SOURCE_REMOVE;
}

static gboolean reasonix_transcript_send_wheel(gpointer data) {
  ReasonixTranscriptSmokeHost *host = data;
  const guint total_ticks = 1200;
  if (host->wheel_tick >= total_ticks) {
    host->wheel_source = 0;
    g_timeout_add(700, reasonix_transcript_request_result, host);
    return G_SOURCE_REMOVE;
  }
  GdkWindow *window = gtk_widget_get_window(GTK_WIDGET(host->web_view));
  if (window != NULL) {
    GtkAllocation allocation;
    gint root_x = 0;
    gint root_y = 0;
    gtk_widget_get_allocation(GTK_WIDGET(host->web_view), &allocation);
    gdk_window_get_origin(window, &root_x, &root_y);
    GdkEvent *event = gdk_event_new(GDK_SCROLL);
    event->scroll.window = g_object_ref(window);
    event->scroll.send_event = TRUE;
    event->scroll.time = GDK_CURRENT_TIME;
    event->scroll.x = allocation.width / 2.0;
    event->scroll.y = allocation.height / 2.0;
    event->scroll.x_root = root_x + event->scroll.x;
    event->scroll.y_root = root_y + event->scroll.y;
    event->scroll.state = 0;
    event->scroll.direction = GDK_SCROLL_SMOOTH;
    event->scroll.delta_x = 0;
    event->scroll.delta_y = 1.0;
    GdkSeat *seat = gdk_display_get_default_seat(gdk_window_get_display(window));
    GdkDevice *pointer = seat != NULL ? gdk_seat_get_pointer(seat) : NULL;
    if (pointer != NULL) {
      gdk_event_set_device(event, pointer);
      gdk_event_set_source_device(event, pointer);
    }
    gtk_widget_event(GTK_WIDGET(host->web_view), event);
    gdk_event_free(event);
  }
  host->wheel_tick += 1;
  return G_SOURCE_CONTINUE;
}

static void reasonix_transcript_message(WebKitUserContentManager *manager,
                                        WebKitJavascriptResult *result,
                                        gpointer data) {
  (void)manager;
  ReasonixTranscriptSmokeHost *host = data;
  JSCValue *value = webkit_javascript_result_get_js_value(result);
  char *message = jsc_value_to_string(value);
  if (message == NULL) return;
  if (strstr(message, "\"type\":\"ready\"") != NULL && host->wheel_source == 0) {
    host->wheel_tick = 0;
    gtk_widget_grab_focus(GTK_WIDGET(host->web_view));
    host->wheel_source = g_timeout_add(16, reasonix_transcript_send_wheel, host);
  } else if (strstr(message, "\"type\":\"result\"") != NULL ||
             strstr(message, "\"type\":\"error\"") != NULL) {
    reasonix_transcript_finish(host, message);
  }
  g_free(message);
}

static void reasonix_transcript_loaded(WebKitWebView *web_view,
                                       WebKitLoadEvent event,
                                       gpointer data) {
  if (event != WEBKIT_LOAD_FINISHED) return;
  ReasonixTranscriptSmokeHost *host = data;
  reasonix_transcript_run_js(host, g_object_get_data(G_OBJECT(web_view), "reasonix-smoke-script"));
}

static gboolean reasonix_transcript_timeout(gpointer data) {
  ReasonixTranscriptSmokeHost *host = data;
  host->safety_source = 0;
  reasonix_transcript_finish(host, "{\"type\":\"error\",\"message\":\"WebKitGTK smoke timed out\"}");
  return G_SOURCE_REMOVE;
}

char *reasonix_transcript_smoke_linux(const char *url, const char *script) {
  if (!gtk_init_check(NULL, NULL)) {
    return strdup("{\"type\":\"error\",\"message\":\"GTK display is unavailable\"}");
  }
  ReasonixTranscriptSmokeHost host = {0};
  WebKitUserContentManager *manager = webkit_user_content_manager_new();
  webkit_user_content_manager_register_script_message_handler(manager, "reasonixNativeSmoke");
  host.web_view = WEBKIT_WEB_VIEW(webkit_web_view_new_with_user_content_manager(manager));
  host.window = gtk_window_new(GTK_WINDOW_TOPLEVEL);
  host.loop = g_main_loop_new(NULL, FALSE);
  gtk_window_set_default_size(GTK_WINDOW(host.window), 1200, 800);
  gtk_container_add(GTK_CONTAINER(host.window), GTK_WIDGET(host.web_view));
  g_object_set_data_full(G_OBJECT(host.web_view), "reasonix-smoke-script", g_strdup(script), g_free);
  g_signal_connect(manager, "script-message-received::reasonixNativeSmoke",
                   G_CALLBACK(reasonix_transcript_message), &host);
  g_signal_connect(host.web_view, "load-changed", G_CALLBACK(reasonix_transcript_loaded), &host);
  gtk_widget_show_all(host.window);
  webkit_web_view_load_uri(host.web_view, url);
  host.safety_source = g_timeout_add_seconds(45, reasonix_transcript_timeout, &host);
  g_main_loop_run(host.loop);
  char *result = strdup(host.result != NULL ? host.result :
                        "{\"type\":\"error\",\"message\":\"WebKitGTK stopped without a result\"}");
  gtk_widget_destroy(host.window);
  while (g_main_context_iteration(NULL, FALSE)) {}
  webkit_user_content_manager_unregister_script_message_handler(manager, "reasonixNativeSmoke");
  g_object_unref(manager);
  g_main_loop_unref(host.loop);
  g_free(host.result);
  return result;
}

#endif
