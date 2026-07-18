namespace Store.Isolator {

    public errordomain ClientError {
        NOT_FOUND,
        PARSE_ERROR,
    }

    private string config_dir () {
        return Path.build_filename (Environment.get_home_dir (), ".config", "isolator");
    }

    public string catalog_path () {
        return Path.build_filename (config_dir (), "package-list.json");
    }

    private string installed_path () {
        return Path.build_filename (config_dir (), "installed.hk");
    }

    /* check_available returns null if `isolator` resolves on PATH, or an
     * error message to show the user if it doesn't — Store is useless
     * without it, so this is checked once at startup. */
    public string? check_available () {
        string? path = Environment.find_program_in_path ("isolator");
        if (path == null) {
            return "The 'isolator' command was not found on PATH. Install Isolator first: https://github.com/HackerOS-Linux-System/Isolator";
        }
        return null;
    }

    /* load_catalog parses the same package-list.json Isolator itself
     * downloads and caches — Store reads it directly rather than
     * reimplementing (or screen-scraping) `isolator search`, since the
     * catalog is just plain JSON already. If the cache doesn't exist yet,
     * the caller should run `isolator refresh` first and retry. */
    public Gee.ArrayList<Package> load_catalog () throws Error {
        var file = File.new_for_path (catalog_path ());
        if (!file.query_exists ()) {
            throw new ClientError.NOT_FOUND ("No cached catalog yet — press Refresh first.");
        }

        var parser = new Json.Parser ();
        parser.load_from_file (catalog_path ());
        var root = parser.get_root ();
        if (root == null || root.get_node_type () != Json.NodeType.ARRAY) {
            throw new ClientError.PARSE_ERROR ("package-list.json is not a JSON array — is it corrupted?");
        }

        var result = new Gee.ArrayList<Package> ();
        foreach (var node in root.get_array ().get_elements ()) {
            if (node.get_node_type () != Json.NodeType.OBJECT) {
                continue;
            }
            var obj = node.get_object ();
            string name = obj.has_member ("name") ? obj.get_string_member ("name") : "";
            string distro = obj.has_member ("distro") ? obj.get_string_member ("distro") : "";
            string type_ = obj.has_member ("type") ? obj.get_string_member ("type") : "cli";
            string[] libs = {};
            if (obj.has_member ("libs")) {
                var libs_array = obj.get_array_member ("libs");
                if (libs_array != null) {
                    foreach (var lib_node in libs_array.get_elements ()) {
                        libs += lib_node.get_string ();
                    }
                }
            }
            if (name != "") {
                result.add (new Package (name, distro, type_, libs));
            }
        }
        return result;
    }

    /* load_installed_names parses installed.hk just enough to get the set
     * of currently-installed package names — a full .hk parser (like the
     * one in isolator's own src/hk.go) is overkill here; Store only ever
     * needs "is X installed?" for exactly one section ([packages]) whose
     * sub-map keys ARE the package names, at nesting depth 1
     * ("-> name", not "--> field"). */
    public Gee.HashSet<string> load_installed_names () {
        var names = new Gee.HashSet<string> ();
        var path = installed_path ();
        if (!FileUtils.test (path, FileTest.EXISTS)) {
            return names;
        }

        string contents;
        try {
            FileUtils.get_contents (path, out contents);
        } catch (Error e) {
            return names;
        }

        bool in_packages_section = false;
        foreach (var raw_line in contents.split ("\n")) {
            var line = raw_line.strip ();
            if (line.length == 0 || line.has_prefix ("!")) {
                continue;
            }
            if (line.has_prefix ("[") && line.has_suffix ("]")) {
                in_packages_section = (line == "[packages]");
                continue;
            }
            if (!in_packages_section) {
                continue;
            }
            // depth-1 entries look like "-> firefox-esr" (no "=>": it's an
            // inline submap declarator, i.e. a package name).
            if (line.has_prefix ("-> ") && !line.has_prefix ("-->") && !("=>" in line)) {
                names.add (line.substring (3).strip ());
            }
        }
        return names;
    }

    public delegate void OutputCallback (string line);
    public delegate void DoneCallback (bool success);
    public delegate void VoidCallback ();

    /* run_async execs `isolator <args>` and streams stdout+stderr back a
     * line at a time via on_output (so the caller can show live progress
     * in a GtkTextView), then reports success via on_done. Uses
     * Subprocess + DataInputStream instead of a blocking call, since this
     * runs on the GTK main loop's thread and must never block it — an
     * `install` can legitimately take a while (image pulls, package
     * manager work). */
    public void run_async (string[] args, owned OutputCallback on_output, owned DoneCallback on_done) {
        string[] full_args = { "isolator" };
        foreach (var a in args) {
            full_args += a;
        }

        try {
            var proc = new Subprocess.newv (
                full_args,
                SubprocessFlags.STDOUT_PIPE | SubprocessFlags.STDERR_MERGE);

            read_stream_lines.begin (proc.get_stdout_pipe (), (owned) on_output, () => {
                proc.wait_async.begin (null, (obj, res) => {
                    bool ok = false;
                    try {
                        proc.wait_async.end (res);
                        ok = proc.get_successful ();
                    } catch (Error e) {
                        ok = false;
                    }
                    on_done (ok);
                });
            });
        } catch (Error e) {
            on_output ("Failed to launch isolator: " + e.message);
            on_done (false);
        }
    }

    private async void read_stream_lines (InputStream stream, owned OutputCallback on_line, owned VoidCallback on_eof) {
        var data_stream = new DataInputStream (stream);
        try {
            while (true) {
                size_t length;
                string? line = yield data_stream.read_line_async (Priority.DEFAULT, null, out length);
                if (line == null) {
                    break;
                }
                on_line (line);
            }
        } catch (Error e) {
            on_line ("(output stream error: " + e.message + ")");
        }
        on_eof ();
    }
}
