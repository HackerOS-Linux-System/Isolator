public class Store.Package : Object {
    public string name;
    public string distro;
    public string type_; // "cli" | "gui" | "de" | "lib" | "system" — "type" is a Vala keyword, hence the trailing underscore
    public string[] libs;

    public Package (string name, string distro, string type_, string[] libs) {
        this.name = name;
        this.distro = distro;
        this.type_ = type_;
        this.libs = libs;
    }

    public string search_haystack () {
        return "%s %s %s".printf (name, distro, type_).down ();
    }
}
