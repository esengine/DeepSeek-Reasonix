#ifdef REASONIX_TRANSCRIPT_SMOKE

#include <gtk/gtk.h>
#include <math.h>
#include <string.h>
#include <webkit2/webkit2.h>

typedef struct {
  GtkWidget *window;
  WebKitWebView *web_view;
  GMainLoop *loop;
  char *result;
  guint wheel_source;
  guint safety_source;
  guint probe_source;
  guint finish_source;
  guint wheel_tick;
  guint finish_wheel_tick;
  guint finish_batch_remaining;
  guint tail_stable_checks;
  gdouble wheel_x;
  gdouble wheel_y;
  gboolean wheel_point_ready;
  gboolean finishing;
  gboolean done;
} ReasonixTranscriptSmokeHost;

static const guint REASONIX_SUSTAINED_WHEEL_TICKS = 1200;
static const guint REASONIX_FINISH_WHEEL_TICKS = 240;
static const guint REASONIX_FINISH_WHEEL_BATCH = 8;

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
  if (host->probe_source != 0) {
    g_source_remove(host->probe_source);
    host->probe_source = 0;
  }
  if (host->finish_source != 0) {
    g_source_remove(host->finish_source);
    host->finish_source = 0;
  }
  host->result = g_strdup(result);
  g_main_loop_quit(host->loop);
}

static void reasonix_transcript_run_js(ReasonixTranscriptSmokeHost *host, const char *script) {
  webkit_web_view_run_javascript(host->web_view, script, NULL, NULL, NULL);
}

static gboolean reasonix_transcript_request_result(gpointer data) {
  ReasonixTranscriptSmokeHost *host = data;
  host->finish_source = 0;
  reasonix_transcript_run_js(host, "window.__reasonixNativeTranscriptSmoke.finish()");
  return G_SOURCE_REMOVE;
}

static gboolean reasonix_transcript_request_tail_status(gpointer data) {
  ReasonixTranscriptSmokeHost *host = data;
  host->probe_source = 0;
  reasonix_transcript_run_js(host, "window.__reasonixNativeTranscriptSmoke.reportTail()");
  return G_SOURCE_REMOVE;
}

static void reasonix_transcript_schedule_result(ReasonixTranscriptSmokeHost *host,
                                                guint delay_ms) {
  if (host->done || host->finish_source != 0) return;
  host->finish_source = g_timeout_add(delay_ms, reasonix_transcript_request_result, host);
}

static void reasonix_transcript_schedule_tail_probe(ReasonixTranscriptSmokeHost *host,
                                                    guint delay_ms) {
  if (host->done || host->probe_source != 0) return;
  host->probe_source = g_timeout_add(delay_ms, reasonix_transcript_request_tail_status, host);
}

static gboolean reasonix_transcript_number_field(const char *message,
                                                  const char *field,
                                                  gdouble *value) {
  const char *field_start = strstr(message, field);
  if (field_start == NULL) return FALSE;
  const char *number_start = field_start + strlen(field);
  char *number_end = NULL;
  const gdouble parsed = g_ascii_strtod(number_start, &number_end);
  if (number_end == number_start || !isfinite(parsed)) return FALSE;
  *value = parsed;
  return TRUE;
}

static void reasonix_transcript_capture_wheel_point(ReasonixTranscriptSmokeHost *host,
                                                     const char *message) {
  gdouble x = 0;
  gdouble y = 0;
  host->wheel_point_ready =
    reasonix_transcript_number_field(message, "\"x\":", &x) &&
    reasonix_transcript_number_field(message, "\"y\":", &y) &&
    x >= 0 && y >= 0;
  if (!host->wheel_point_ready) return;
  host->wheel_x = x;
  host->wheel_y = y;
}

static void reasonix_transcript_dispatch_wheel(ReasonixTranscriptSmokeHost *host) {
  GdkWindow *window = gtk_widget_get_window(GTK_WIDGET(host->web_view));
  if (window == NULL) return;
  GtkAllocation allocation;
  gint root_x = 0;
  gint root_y = 0;
  gtk_widget_get_allocation(GTK_WIDGET(host->web_view), &allocation);
  gdk_window_get_origin(window, &root_x, &root_y);
  GdkEvent *event = gdk_event_new(GDK_SCROLL);
  event->scroll.window = g_object_ref(window);
  event->scroll.send_event = TRUE;
  event->scroll.time = GDK_CURRENT_TIME;
  event->scroll.x = host->wheel_point_ready
    ? CLAMP(host->wheel_x, 0.0, MAX(0.0, allocation.width - 1.0))
    : allocation.width / 2.0;
  event->scroll.y = host->wheel_point_ready
    ? CLAMP(host->wheel_y, 0.0, MAX(0.0, allocation.height - 1.0))
    : allocation.height / 2.0;
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

static gboolean reasonix_transcript_send_wheel(gpointer data) {
  ReasonixTranscriptSmokeHost *host = data;
  if (!host->finishing && host->wheel_tick >= REASONIX_SUSTAINED_WHEEL_TICKS) {
    host->wheel_source = 0;
    host->finishing = TRUE;
    reasonix_transcript_schedule_tail_probe(host, 700);
    return G_SOURCE_REMOVE;
  }
  if (host->finishing && host->finish_batch_remaining == 0) {
    host->wheel_source = 0;
    reasonix_transcript_schedule_tail_probe(host, 200);
    return G_SOURCE_REMOVE;
  }
  reasonix_transcript_dispatch_wheel(host);
  if (host->finishing) {
    host->finish_wheel_tick += 1;
    host->finish_batch_remaining -= 1;
  } else {
    host->wheel_tick += 1;
  }
  return G_SOURCE_CONTINUE;
}

static void reasonix_transcript_start_finish_batch(ReasonixTranscriptSmokeHost *host) {
  if (host->done || host->wheel_source != 0) return;
  const guint remaining = host->finish_wheel_tick < REASONIX_FINISH_WHEEL_TICKS
    ? REASONIX_FINISH_WHEEL_TICKS - host->finish_wheel_tick
    : 0;
  host->finish_batch_remaining = MIN(REASONIX_FINISH_WHEEL_BATCH, remaining);
  if (host->finish_batch_remaining == 0) {
    reasonix_transcript_schedule_result(host, 700);
    return;
  }
  host->wheel_source = g_timeout_add(16, reasonix_transcript_send_wheel, host);
}

static gboolean reasonix_transcript_tail_reached(const char *message) {
  const char *distance_field = strstr(message, "\"distance\":");
  if (distance_field == NULL || strstr(message, "\"mode\":\"tail-follow\"") == NULL) {
    return FALSE;
  }
  const char *distance_value = distance_field + strlen("\"distance\":");
  char *distance_end = NULL;
  const double distance = g_ascii_strtod(distance_value, &distance_end);
  if (distance_end == distance_value) return FALSE;
  return distance >= 0 && distance <= 4;
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
    reasonix_transcript_capture_wheel_point(host, message);
    host->wheel_tick = 0;
    host->finish_wheel_tick = 0;
    host->finish_batch_remaining = 0;
    host->tail_stable_checks = 0;
    host->finishing = FALSE;
    gtk_widget_grab_focus(GTK_WIDGET(host->web_view));
    host->wheel_source = g_timeout_add(16, reasonix_transcript_send_wheel, host);
  } else if (strstr(message, "\"type\":\"tail-status\"") != NULL) {
    if (reasonix_transcript_tail_reached(message)) {
      host->tail_stable_checks += 1;
      if (host->tail_stable_checks >= 2) {
        reasonix_transcript_schedule_result(host, 700);
      } else {
        reasonix_transcript_schedule_tail_probe(host, 200);
      }
    } else {
      host->tail_stable_checks = 0;
      reasonix_transcript_start_finish_batch(host);
    }
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
