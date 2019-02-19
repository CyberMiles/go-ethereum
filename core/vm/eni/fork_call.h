
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdbool.h>
#include <unistd.h>
#include <inttypes.h>
#include <errno.h>
#include <assert.h>
#include <signal.h>
#include <fcntl.h>
#include <time.h>
#include <sys/prctl.h>
#include <sys/syscall.h>
#include <sys/types.h>
#include <sys/wait.h>
#include <sys/epoll.h>
#include <sys/timerfd.h>
#include <linux/seccomp.h>

// TODO: Maybe we need to take different measure for two kinds of errors:
//   1. go-ethereum's fault. ex. no enough fd
//   2. libeni's fault.
// And we have different ways to handle them. (ex. kill geth v.s mark the tx as invalid)
#define ENI_ERROR_CODES(X)                                                    \
    X(0,    ENI_SUCCESS,       "Success")                                     \
    X(11,   ENI_FAILURE,       "An unclassified occurred")                    \
    X(12,   ENI_RESOURCE_BUSY, "Failed to perform some syscalls")             \
    X(13,   ENI_SECCOMP_FAIL,  "Failed to create sandbox for safe execution") \
    X(21,   ENI_TLE,           "Execution timeout")                           \
    X(22,   ENI_KILLED,        "ENI operation got killed")                    \
    X(23,   ENI_SEGFAULT,      "ENI operation segmentation fault")            \
    X(24,   ENI_NULL_RESULT,   "ENI operation returns NULL")                  \

#define ENI_ERR_ENUM(ID, NAME, DESC) NAME = ID,
#define ENI_ERR_TEXT(ID, NAME, TEXT) case ID: return TEXT;

// TODO: Need further refinement about how to classify errors
bool is_libeni_fault(int code) {
    return code >= 20;
}

enum { ENI_ERROR_CODES(ENI_ERR_ENUM) };

const char* eni_error_msg(int code) {
    switch (code) { ENI_ERROR_CODES(ENI_ERR_TEXT) }
    return "Invalid error code";
}

// represents a function in libeni
typedef void* eni_function;

// represents data returned by eni_function
typedef void* eni_return_data;

// determine how long the result is, the reason that
// we need this function is, we cannot directly find
// the size of data void* is pointing to
typedef size_t (*eni_result_length_finder)(eni_return_data);

size_t gas_result_length(eni_return_data p) {
    // libeni returns int64_t* for gas function
    return sizeof(int64_t);
}

size_t run_result_length(eni_return_data p) {
    // libeni returns char* for run function
    return strlen((char*)p)+1;
}

// A function that knows the actual type of eni functions
typedef eni_return_data (*eni_executor)(eni_function, char*);

eni_return_data eni_gas_executor(eni_function f, char* arg) {
    typedef int64_t* (*func_gas)(char*);
    return ((func_gas)f)(arg);
}
eni_return_data eni_run_executor(eni_function f, char* arg) {
    typedef char* (*func_run)(char*);
    return ((func_run)f)(arg);
}

eni_return_data fork_call(eni_result_length_finder, eni_executor, eni_function f, char* args_text, int *status);
eni_return_data wait_and_read_from_child(int pid, int pfd, int* eni_status);
int eni_fork_child(eni_result_length_finder, eni_executor, eni_function f, char* args_text, int pfd);

// f should be op_gas()
uint64_t fork_gas(void* f, char *argsText, int* status) {
    uint64_t* ret = fork_call(gas_result_length, eni_gas_executor, (eni_function)f, argsText, status);
    if (ret == NULL) return 0;
    uint64_t val = *ret;
    free(ret);
    return val;
}

// f should be op_run()
char* fork_run(void* f, char *argsText, int* status){
    return (char*) fork_call(run_result_length, eni_run_executor, (eni_function)f, argsText, status);
}

eni_return_data fork_call(
    eni_result_length_finder get_result_len,
    eni_executor exe,
    eni_function f,
    char* args_text,
    int *status
)
{
    // First, create a pipe so that child process can send ENI execution result
    // to parent process
    int pfd[2];
    if (pipe(pfd) == -1) {
        *status = ENI_RESOURCE_BUSY;
        return NULL;
    }
    if (fcntl(pfd[0], F_SETFL, O_NONBLOCK) < 0) {
        *status = ENI_FAILURE;
        close(pfd[0]);
        close(pfd[1]);
        return NULL;
    }

    int pid;
    if ((pid = eni_fork_child(get_result_len, exe, f, args_text, pfd[1])) < 0) {
        *status = ENI_FAILURE; // failed to fork a child
        close(pfd[0]);
        close(pfd[1]);
        return NULL;
    }
    close(pfd[1]);

    int eni_read_status = ENI_SUCCESS;
    eni_return_data child_exe_result = wait_and_read_from_child(pid, pfd[0], &eni_read_status);
    close(pfd[0]);

    if (!child_exe_result) {
        assert(eni_read_status != ENI_SUCCESS);
        *status = eni_read_status;
        return NULL;
    }

    int child_status;
    while (true) {
        pid_t waitpid_result = waitpid(pid, &child_status, WNOHANG);
        if (waitpid_result == -1) {
            *status = ENI_FAILURE;
            free(child_exe_result);
            return NULL;
        }
        else if (waitpid_result == 0) {
            // On some systems, waitpid can temporary return 0 even if OS already closed
            // file descriptor opened by child process.
            fprintf(stderr, "ENI Warning: Child not fully terminated yet, retrying...\n");
        }
        else {
            break;
        }
    }

    if (WIFEXITED(child_status)) {
        // the child terminated normally, that is, by calling exit.
        if (WEXITSTATUS(child_status) != ENI_SUCCESS) {
            *status = WEXITSTATUS(child_status);
            return NULL;
        }
        else {
            *status = ENI_SUCCESS;
            return child_exe_result;
        }
    }
    else if (WIFSIGNALED(child_status)) {
        switch (WTERMSIG(child_status)) {
            case SIGSEGV:
                *status = ENI_SEGFAULT;
                break;
            case SIGKILL:
                *status = ENI_KILLED; // maybe it calls forbidden syscalls?
                break;
            default:
                *status = ENI_FAILURE;
        }
        free(child_exe_result);
        return NULL;
    }
    else {
        // TODO: is this even possible?
        // "not terminated normally" and "not terminated by signal" happens at the same time
        assert(false);
    }
}

int set_up_sandbox(int pipefd) {
    if (FD_SETSIZE > 1e+4) {
        // Unless user specifically configured and recompile the kernel himself/herself,
        // FD_SETSIZE should equal to 1024 and checking the status of all FDs should be an acceptable impl
        fprintf(stderr, "ENI Warning: FD_SETSIZE=%d, which is abnormally big\n", FD_SETSIZE);
    }
    for (int i = 0 ; i != FD_SETSIZE ; i++) {
        if (i == pipefd) {
            // we will use this file descriptor to communicate with parent process
            assert(fcntl(i, F_GETFL) != -1);
            continue;
        }
        if (fcntl(i, F_GETFL) != -1)
            if (close(i) == -1)
                return ENI_RESOURCE_BUSY;
    }
    if (prctl(PR_SET_SECCOMP, SECCOMP_MODE_STRICT) != 0)
        return ENI_SECCOMP_FAIL;
    return 0;
}

// create a fd that will be avaliable to be read after 3 seconds
int create_eni_timerfd() {
    int tfd = timerfd_create(CLOCK_MONOTONIC, 0);
    if (tfd == -1)
        return -1;
    struct itimerspec timeout_value;
    memset(&timeout_value, 0, sizeof(timeout_value));
    timeout_value.it_value.tv_sec = 3;
    if (timerfd_settime(tfd, 0, &timeout_value, NULL) == -1)
        return -1;
    return tfd;
}

// @return the data, could be NULL
// @param eni_status When the returned pointer is NULL, `eni_status` will be set to corresponding error code.
// This function will keep trying to read from `pfd`,
// until timeout reached or `pfd` reached EOF (EOF implies another end of the pipe was closed)
eni_return_data wait_and_read_from_child(int pid, int pfd, int* eni_status) {
    int ret_len = 0, ret_cap = 32;
    eni_return_data ret = malloc(ret_cap);
    if (!ret) goto unclassified_error;

    int tfd = -1, epfd = -1;
    if ((tfd = create_eni_timerfd()) == -1)
        goto unclassified_error;
    if ((epfd = epoll_create1(0)) == -1)
        goto unclassified_error;

    struct epoll_event epev;

    memset(&epev, 0, sizeof(struct epoll_event));
    epev.events = EPOLLIN | EPOLLET;
    epev.data.fd = pfd;
    if (epoll_ctl(epfd, EPOLL_CTL_ADD, pfd, &epev))
        goto unclassified_error;

    memset(&epev, 0, sizeof(struct epoll_event));
    epev.events = EPOLLIN;
    epev.data.fd = tfd;
    if (epoll_ctl(epfd, EPOLL_CTL_ADD, tfd, &epev))
        goto unclassified_error;

/*
  Success
 +-------+
 |       v                    +-------------+
 |    +-----------+  EAGAIN   |epoll_wait   |
 +----+ read(pfd) +---------->+pfd (ET mode)|
      +--+----+---+           |tfd          |
         |    ^               +---+------+--+
         |    |                   |      |
     EOF |    +-------------------+      | tfd triggered
         |        pfd triggered          |
         v                               v
     return ret;                      ENI_TLE
*/
    while (true) {
        if (ret_cap == ret_len) {
            ret_cap *= 2;
            eni_return_data new_ret = realloc(ret, ret_cap);
            if (!new_ret)
                goto unclassified_error;
            else
                ret = new_ret;
        }
        int nread = read(pfd, ret + ret_len, ret_cap - ret_len);
        if (nread > 0) { // Success
            ret_len += nread;
            continue;
        }
        else if (nread == 0) { // EOF
            goto end;
        }
        else { // EAGAIN
            if (errno != EAGAIN && errno != EWOULDBLOCK) goto unclassified_error;
            struct epoll_event ev;
            int num_ev = epoll_wait(epfd, &ev, 1, -1);
            if (num_ev != 1) goto error;
            if (ev.data.fd == pfd) {
                continue;
            }
            else if (ev.data.fd == tfd) {
                kill(pid, SIGKILL); // it is this functions's caller's responsibility to waitpid
                *eni_status = ENI_TLE;
                goto error;
            }
            else goto unclassified_error;
        }
    }

unclassified_error:
    *eni_status = ENI_FAILURE;
error:
    free(ret);
    ret = NULL;
end:
    if (tfd != -1) close(tfd);
    if (epfd != -1) close(epfd);
    return ret;
}

// @return the child pid, if less than zero, means failed to fork()
int eni_fork_child(
    eni_result_length_finder get_result_len,
    eni_executor exe,
    eni_function f,
    char* args_text,
    int pfd
)
{
    int pid = fork();
    if (pid) return pid;

    // child process' code starts from here

    int errnum;
    if ((errnum = set_up_sandbox(pfd)) != ENI_SUCCESS)
        exit(errnum);
    void* result = exe(f, args_text);
    if (!result)
        syscall(SYS_exit, ENI_NULL_RESULT);
    int tot_write = 0;
    int len = get_result_len(result);
    while (tot_write < len) {
        int nwrite = write(pfd, result, len-tot_write);
        if (nwrite <= 0)
            syscall(SYS_exit, ENI_RESOURCE_BUSY);
        tot_write += nwrite;
    }
    // cannot use libc's exit() because it calls exit_group() which is forbidden by seccomp
    syscall(SYS_exit, ENI_SUCCESS);
    assert("not reachable code" && false); // shut up the compile error about not returning a value
}

