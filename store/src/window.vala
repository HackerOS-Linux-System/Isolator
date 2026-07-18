[GtkTemplate (ui = "/org/hackeros/IsolatorStore/window.ui")]
public class Store.IsolatorStoreWindow : Adw.ApplicationWindow {
    [GtkChild] private unowned Adw.ToastOverlay toast_overlay;
    [GtkChild] private unowned Gtk.SearchEntry search_entry;
    [GtkChild] private unowned Gtk.Button refresh_button;
    [GtkChild] private unowned Gtk.ListBox results_list;
    [GtkChild] private unowned Adw.StatusPage empty_status;
    [GtkChild] private unowned Gtk.Box detail_box;
    [GtkChild] private unowned Gtk.Label detail_title;
    [GtkChild] private unowned Gtk.Label detail_meta;
    [GtkChild] private unowned Gtk.Label detail_libs;
    [GtkChild] private unowned Gtk.Button install_button;
    [GtkChild] private unowned Gtk.Button remove_button;
    [GtkChild] private unowned Gtk.CheckButton isolated_check;
    [GtkChild] private unowned Gtk.Label status_label;
    [GtkChild] private unowned Gtk.TextView log_view;

    private Gee.ArrayList<Package> catalog = new Gee.ArrayList<Package> ();
    private Gee.HashSet<string> installed_names = new Gee.HashSet<string> ();
    private Package? selected_package = null;
    private bool operation_in_progress = false;

    public IsolatorStoreWindow (Adw.Application app) {
        Object (application: app);

        var missing = Isolator.check_available ();
        if (missing != null) {
            empty_status.set_title ("isolator not found");
            empty_status.set_description (missing);
            search_entry.set_sensitive (false);
            refresh_button.set_sensitive (false);
            return;
        }

        search_entry.search_changed.connect (on_search_changed);
        refresh_button.clicked.connect (on_refresh_clicked);
        results_list.row_selected.connect (on_row_selected);
        install_button.clicked.connect (on_install_clicked);
        remove_button.clicked.connect (on_remove_clicked);

        reload_state ();
    }

    private void reload_state () {
        installed_names = Isolator.load_installed_names ();
        try {
            catalog = Isolator.load_catalog ();
            empty_status.set_description ("Search for a package on the left, or press Refresh to update the catalog.");
            populate_results ("");
        } catch (Error e) {
            catalog = new Gee.ArrayList<Package> ();
            empty_status.set_title ("No catalog yet");
            empty_status.set_description (e.message);
        }
    }

    private void on_refresh_clicked () {
        set_busy (true, "Refreshing catalog…");
        clear_log ();
        Isolator.run_async ({ "refresh" }, (line) => {
            append_log (line);
        }, (success) => {
            set_busy (false, success ? "Catalog refreshed." : "Refresh failed — see log.");
            reload_state ();
            if (success) {
                toast_overlay.add_toast (new Adw.Toast ("Catalog refreshed"));
            }
        });
    }

    private void on_search_changed () {
        populate_results (search_entry.get_text ());
    }

    private void populate_results (string query) {
        var child = results_list.get_first_child ();
        while (child != null) {
            var next = child.get_next_sibling ();
            results_list.remove (child);
            child = next;
        }

        string needle = query.down ().strip ();
        int shown = 0;
        foreach (var pkg in catalog) {
            if (needle != "" && !(needle in pkg.search_haystack ())) {
                continue;
            }
            results_list.append (new PackageRow (pkg));
            shown++;
            if (shown >= 200) {
                // A simple cap so a broad/empty query on a 5000+ package
                // catalog doesn't build thousands of rows at once — the
                // person narrows the search instead. Not virtualized
                // scrolling, just a sane bound for a small utility app.
                break;
            }
        }
    }

    private void on_row_selected (Gtk.ListBoxRow? row) {
        if (row == null) {
            detail_box.set_visible (false);
            empty_status.set_visible (true);
            selected_package = null;
            return;
        }
        var package_row = (PackageRow) row;
        selected_package = package_row.package;

        empty_status.set_visible (false);
        detail_box.set_visible (true);
        detail_title.set_label (selected_package.name);
        detail_meta.set_label ("%s  ·  %s".printf (selected_package.distro, selected_package.type_));

        if (selected_package.libs.length > 0) {
            detail_libs.set_label ("Dependencies: " + string.joinv (", ", selected_package.libs));
            detail_libs.set_visible (true);
        } else {
            detail_libs.set_visible (false);
        }

        bool is_installed = installed_names.contains (selected_package.name);
        install_button.set_sensitive (!is_installed && !operation_in_progress);
        remove_button.set_sensitive (is_installed && !operation_in_progress);
        isolated_check.set_sensitive (!is_installed && !operation_in_progress);
        status_label.set_label (is_installed ? "Installed" : "Not installed");
        clear_log ();
    }

    private void on_install_clicked () {
        if (selected_package == null) {
            return;
        }
        string[] args = { "install", selected_package.name };
        if (isolated_check.get_active ()) {
            args += "--isolated";
        }
        run_operation (args, "Installing " + selected_package.name + "…");
    }

    private void on_remove_clicked () {
        if (selected_package == null) {
            return;
        }
        run_operation ({ "remove", selected_package.name }, "Removing " + selected_package.name + "…");
    }

    private void run_operation (string[] args, string busy_message) {
        set_busy (true, busy_message);
        clear_log ();
        Isolator.run_async (args, (line) => {
            append_log (line);
        }, (success) => {
            set_busy (false, success ? "Done." : "Failed — see log below.");
            installed_names = Isolator.load_installed_names ();
            if (selected_package != null) {
                bool is_installed = installed_names.contains (selected_package.name);
                install_button.set_sensitive (!is_installed);
                remove_button.set_sensitive (is_installed);
                status_label.set_label (is_installed ? "Installed" : "Not installed");
            }
            toast_overlay.add_toast (new Adw.Toast (success ? "Operation completed" : "Operation failed"));
        });
    }

    private void set_busy (bool busy, string message) {
        operation_in_progress = busy;
        install_button.set_sensitive (!busy && selected_package != null && !installed_names.contains (selected_package.name));
        remove_button.set_sensitive (!busy && selected_package != null && installed_names.contains (selected_package.name));
        refresh_button.set_sensitive (!busy);
        status_label.set_label (message);
    }

    private void clear_log () {
        log_view.get_buffer ().set_text ("", 0);
    }

    private void append_log (string line) {
        var buffer = log_view.get_buffer ();
        Gtk.TextIter end;
        buffer.get_end_iter (out end);
        buffer.insert (ref end, line + "\n", -1);
        log_view.scroll_to_iter (end, 0.0, false, 0.0, 0.0);
    }
}
