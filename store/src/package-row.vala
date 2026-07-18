public class Store.PackageRow : Gtk.ListBoxRow {
    public Package package { get; private set; }

    public PackageRow (Package package) {
        this.package = package;

        var box = new Gtk.Box (Gtk.Orientation.VERTICAL, 2) {
            margin_top = 6,
            margin_bottom = 6,
            margin_start = 10,
            margin_end = 10,
        };

        var name_label = new Gtk.Label (package.name) {
            xalign = 0,
            css_classes = { "heading" },
        };

        var meta_label = new Gtk.Label ("%s · %s".printf (package.distro, package.type_)) {
            xalign = 0,
            css_classes = { "dim-label", "caption" },
        };

        box.append (name_label);
        box.append (meta_label);
        this.child = box;
    }
}
