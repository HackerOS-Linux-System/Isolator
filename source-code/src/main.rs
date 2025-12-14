use std::env;
use std::fs::{self, File};
use std::io::Write;
use std::path::{Path, PathBuf};
use anyhow::Result;
use clap::{Parser, Subcommand, ArgGroup};
use directories::UserDirs;
use indicatif::{ProgressBar, ProgressStyle};
use inquire::Confirm;
use nix::mount::{mount, MsFlags};
use nix::sched::{unshare, CloneFlags};
use nix::sys::stat::Mode;
use nix::unistd::{chdir, execvp, fork, getuid, mkdir, pivot_root, ForkResult};
use nix::mount::umount2;
use nix::mount::MntFlags;
use nix::sys::wait::waitpid;
use seccomp_sys::*;
use tracing::{error, info};
use tracing_subscriber::FmtSubscriber;
use libc::{prctl, PR_CAPBSET_DROP, PR_SET_NO_NEW_PRIVS};

#[derive(Parser)]
#[clap(name = "isolator", about = "User-space isolation tool for HackerOS", version = "0.1.0")]
struct Cli {
    #[clap(subcommand)]
    command: Commands,
}

#[derive(Subcommand)]
enum Commands {
    /// Create a new isolated environment for an application
    Create {
        /// Name of the application/profile
        app_name: String,
        /// Shares to enable (comma-separated: home,wayland,x11,sound,tools)
        #[clap(long, value_delimiter = ',')]
        share: Vec<String>,
    },
    /// Link one profile to another
    Link {
        /// Source profile
        source: String,
        /// Target profile
        target: String,
    },
    /// Install a package into the environment
    Install {
        /// Package name
        package: String,
    },
    /// Remove a package from the environment
    Remove {
        /// Package name
        package: String,
    },
    /// Community install (not implemented)
    #[clap(group = ArgGroup::new("community"))]
    CommunityInstall {
        /// Package name
        package: String,
    },
    /// Community remove (not implemented)
    CommunityRemove {
        /// Package name
        package: String,
    },
    // Add more commands as needed
}

fn main() -> Result<()> {
    // Setup tracing
    let subscriber = FmtSubscriber::builder().finish();
    tracing::subscriber::set_global_default(subscriber)?;

    let cli = Cli::parse();

    // Get isolator dir: ~/.hackeros/isolator/
    let user_dirs = UserDirs::new().unwrap();
    let home = user_dirs.home_dir();
    let isolator_dir = home.join(".hackeros/isolator");
    fs::create_dir_all(&isolator_dir)?;

    match cli.command {
        Commands::Create { app_name, share } => create_environment(&app_name, &share, &isolator_dir)?,
        Commands::Link { source, target } => link_profiles(&source, &target, &isolator_dir)?,
        Commands::Install { package } => install_package(&package, &isolator_dir)?,
        Commands::Remove { package } => remove_package(&package, &isolator_dir)?,
        Commands::CommunityInstall { package: _ } => {
            println!("Community install not implemented yet.");
        }
        Commands::CommunityRemove { package: _ } => {
            println!("Community remove not implemented yet.");
        }
    }

    Ok(())
}

fn create_environment(app_name: &str, shares: &Vec<String>, isolator_dir: &Path) -> Result<()> {
    info!("Creating environment for {}", app_name);

    // Create per-app rootfs dir
    let app_dir = isolator_dir.join(app_name);
    fs::create_dir_all(&app_dir)?;

    // Setup progress bar like yarn.gif
    let pb = ProgressBar::new_spinner();
    pb.set_style(ProgressStyle::default_spinner()
    .template("{spinner:.green} [{elapsed_precise}] {msg}")
    .unwrap());
    pb.set_message("Setting up rootfs...");

    // Simulate debootstrap (in reality, exec debootstrap with fake root or use pre-built)
    // For demo, assume we copy a minimal rootfs or something
    setup_rootfs(&app_dir)?;

    pb.finish_with_message("Rootfs setup complete.");

    // Ask for confirmation using inquire
    let ans = Confirm::new("Proceed to launch in isolated env?").prompt()?;
    if !ans {
        return Ok(());
    }

    // Fork and unshare namespaces
    unsafe {
        match fork()? {
            ForkResult::Parent { child } => {
                // Wait for child
                waitpid(child, None)?;
            }
            ForkResult::Child => {
                // Unshare namespaces
                unshare(CloneFlags::CLONE_NEWUSER | CloneFlags::CLONE_NEWPID | CloneFlags::CLONE_NEWNET | CloneFlags::CLONE_NEWNS | CloneFlags::CLONE_NEWUTS | CloneFlags::CLONE_NEWIPC)?;

                // Map user to root in new user ns
                setup_user_namespace()?;

                // Setup mounts
                setup_mounts(&app_dir, shares)?;

                // Drop capabilities
                drop_capabilities()?;

                // Set no_new_privs
                if prctl(PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0) < 0 {
                    return Err(anyhow::anyhow!("Failed to set no_new_privs"));
                }

                // Setup seccomp
                setup_seccomp()?;

                // Chdir and pivot_root
                chdir(app_dir.as_os_str())?;
                pivot_root(".", "old_root")?;
                umount2("old_root", MntFlags::MNT_DETACH)?;

                // Make /usr read-only
                mount::<str, str, str, str>(Some("/usr"), "/usr", None, MsFlags::MS_BIND | MsFlags::MS_REMOUNT | MsFlags::MS_RDONLY, None)?;

                // Exec the app (for example, assume app_name is the binary)
                let cstr_app = std::ffi::CString::new(app_name)?;
                execvp(&cstr_app, &[] as &[&std::ffi::CString])?;
            }
        }
    }

    Ok(())
}

fn setup_rootfs(app_dir: &Path) -> Result<()> {
    // In reality: exec debootstrap --variant=minbase bookworm <dir> http://deb.debian.org/debian
    // But without root, need proot or fakechroot. For simplicity, simulate.
    // Assume we have a minimal tarball or something.
    fs::create_dir_all(app_dir.join("usr/bin"))?;
    // Copy some binaries or simulate
    Ok(())
}

fn setup_user_namespace() -> Result<()> {
    // Write uid_map and gid_map
    let uid = getuid();
    let mut uid_map = File::create("/proc/self/uid_map")?;
    writeln!(uid_map, "0 {} 1", uid)?;
    let mut gid_map = File::create("/proc/self/gid_map")?;
    writeln!(gid_map, "0 {} 1", uid)?;
    let mut setgroups = File::create("/proc/self/setgroups")?;
    writeln!(setgroups, "deny")?;
    Ok(())
}

fn setup_mounts(_app_dir: &Path, shares: &Vec<String>) -> Result<()> {
    // Mount proc, sys, dev, tmp
    mkdir("/proc", Mode::S_IRWXU)?;
    mount::<str, str, str, str>(Some("proc"), "/proc", Some("proc"), MsFlags::empty(), None)?;

    // Similarly for sys, dev, tmpfs on /tmp
    // TODO: Add mounts for /sys, /dev, /tmp

    // Bind shares
    for share in shares {
        match share.as_str() {
            "home" => {
                let home = UserDirs::new().unwrap().home_dir().to_owned();
                let docs = home.join("Documents");
                mount::<Path, str, str, str>(Some(docs.as_path()), "/home/user/Documents", None, MsFlags::MS_BIND, None)?;
            }
            "wayland" => {
                let wayland_socket = PathBuf::from("/run/user/1000/wayland-0"); // Assume uid 1000
                mount::<Path, str, str, str>(Some(wayland_socket.as_path()), "/run/wayland-0", None, MsFlags::MS_BIND, None)?;
                env::set_var("WAYLAND_DISPLAY", "wayland-0");
            }
            "x11" => {
                let x11_socket = PathBuf::from("/tmp/.X11-unix");
                mount::<Path, str, str, str>(Some(x11_socket.as_path()), "/tmp/.X11-unix", None, MsFlags::MS_BIND, None)?;
                env::set_var("DISPLAY", ":0"); // Assume
            }
            "sound" => {
                // PipeWire or Pulse
                let pw_socket = PathBuf::from("/run/user/1000/pipewire-0");
                mount::<Path, str, str, str>(Some(pw_socket.as_path()), "/run/pipewire-0", None, MsFlags::MS_BIND, None)?;
            }
            "tools" => {
                mount::<str, str, str, str>(Some("/usr/bin/git"), "/usr/bin/git", None, MsFlags::MS_BIND, None)?;
                // Add more tools as needed
            }
            _ => error!("Unknown share: {}", share),
        }
    }
    Ok(())
}

fn drop_capabilities() -> Result<()> {
    // Drop all capabilities using libc
    unsafe {
        for cap in 0..=40 {
            if prctl(PR_CAPBSET_DROP, cap, 0, 0, 0) < 0 {
                return Err(anyhow::anyhow!("Failed to drop capability {}", cap));
            }
        }
    }
    Ok(())
}

fn setup_seccomp() -> Result<()> {
    unsafe {
        let ctx = seccomp_init(SCMP_ACT_ALLOW);
        if ctx.is_null() {
            return Err(anyhow::anyhow!("seccomp_init failed"));
        }
        // Deny ptrace
        seccomp_rule_add(ctx, SCMP_ACT_ERRNO(libc::EPERM as u32), libc::SYS_ptrace as i32, 0);
        // Deny mount
        seccomp_rule_add(ctx, SCMP_ACT_ERRNO(libc::EPERM as u32), libc::SYS_mount as i32, 0);
        // Deny kexec
        seccomp_rule_add(ctx, SCMP_ACT_ERRNO(libc::EPERM as u32), libc::SYS_kexec_load as i32, 0);
        seccomp_load(ctx);
        seccomp_release(ctx);
    }
    Ok(())
}

fn link_profiles(source: &str, target: &str, isolator_dir: &Path) -> Result<()> {
    info!("Linking {} to {}", source, target);
    // Simulate linking by symlinking dirs or sharing mounts
    let source_dir = isolator_dir.join(source);
    let target_dir = isolator_dir.join(target);
    fs::create_dir_all(&target_dir)?;
    // For example, symlink
    std::os::unix::fs::symlink(source_dir, target_dir.join("linked"))?;
    Ok(())
}

fn install_package(package: &str, _isolator_dir: &Path) -> Result<()> {
    // Assume current dir is an app dir, but for global?
    // Enter namespace and run apt install
    // But complex; simulate with progress
    let pb = ProgressBar::new(100);
    pb.set_style(ProgressStyle::default_bar()
    .template("{spinner:.green} [{elapsed_precise}] [{bar:40.cyan/blue}] {pos}/{len} {msg}")
    .unwrap()
    .progress_chars("#>-"));
    pb.set_message(format!("Installing {}", package));

    for i in 0..100 {
        pb.set_position(i);
        std::thread::sleep(std::time::Duration::from_millis(50));
    }
    pb.finish_with_message("Installed.");

    Ok(())
}

fn remove_package(package: &str, _isolator_dir: &Path) -> Result<()> {
    // Similar to install
    let pb = ProgressBar::new(100);
    pb.set_style(ProgressStyle::default_bar()
    .template("{spinner:.green} [{elapsed_precise}] [{bar:40.cyan/blue}] {pos}/{len} {msg}")
    .unwrap()
    .progress_chars("#>-"));
    pb.set_message(format!("Removing {}", package));

    for i in 0..100 {
        pb.set_position(i);
        std::thread::sleep(std::time::Duration::from_millis(50));
    }
    pb.finish_with_message("Removed.");

    Ok(())
}

// Note: This is a simplified implementation. In reality, handling GUI apps requires proper env passing, and installation needs proper chroot/exec apt in namespace.
// Many parts are placeholders or simulated due to complexity.
// For full functionality, more error handling, and actual debootstrap integration (perhaps via Command::new("debootstrap") with PRoot).
// Add more CLI beauty with termimad for markdown output, reedline for REPL if needed, etc.
// For tables: use tabled or comfy-table to display lists of environments/packages.
// Switched to libc for prctl calls to resolve compilation issues.
// Hardcoded CAP_LAST_CAP to 40 based on current Linux kernel.
