public class Store.Application : Adw.Application {
    public Application () {
        Object (
            application_id: "org.hackeros.IsolatorStore",
            flags: ApplicationFlags.DEFAULT_FLAGS
        );
    }

    protected override void activate () {
        var window = new IsolatorStoreWindow (this);
        window.present ();
    }
}

public static int main (string[] args) {
    var app = new Store.Application ();
    return app.run (args);
}
